use percent_encoding::{NON_ALPHANUMERIC, utf8_percent_encode};
use regex::Regex;
use serde::Deserialize;
use serde_json::{Value, json};
use std::time::Duration;

pub const MANAGED_KINDS: [&str; 5] = [
    "openai-compatibility",
    "claude-api-key",
    "codex-api-key",
    "xai-api-key",
    "vertex-api-key",
];
const MANAGEMENT_LIMIT: u64 = 8 * 1024 * 1024;
const INVENTORY_LIMIT: u64 = 32 * 1024 * 1024;

#[derive(Default, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Filter {
    #[serde(default)]
    pub include: Vec<String>,
    #[serde(default)]
    pub exclude: Vec<String>,
    #[serde(default)]
    pub path: Option<String>,
}

#[derive(Clone, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct RequestOptions {
    pub retries: u32,
    pub retry_delay_ms: u64,
    pub timeout_ms: u64,
}

impl Default for RequestOptions {
    fn default() -> Self {
        Self {
            retries: 3,
            retry_delay_ms: 1000,
            timeout_ms: 30_000,
        }
    }
}

impl RequestOptions {
    pub fn validate(&self) -> Result<()> {
        if self.timeout_ms == 0
            || self.retries > 30
            || self
                .retry_delay_ms
                .checked_mul(1u64 << self.retries)
                .is_none()
        {
            return Err("invalid request timeout or retry budget");
        }
        Ok(())
    }
}

#[derive(Debug, PartialEq)]
pub enum Outcome {
    Updated,
    Unchanged,
    Unconfirmed,
}

// CPA's management writer cannot safely process concurrent updates, even for
// different channels. Keep fetches parallel; serialize the fresh-read/write phase.
static CPA_WRITER: std::sync::Mutex<()> = std::sync::Mutex::new(());

// Errors deliberately exclude request URLs, response bodies and credentials.
type Result<T> = std::result::Result<T, &'static str>;

struct Cpa<'a> {
    agent: ureq::Agent,
    base: &'a str,
    key: &'a str,
    options: &'a RequestOptions,
}

impl<'a> Cpa<'a> {
    fn new(base: &'a str, key: &'a str, options: &'a RequestOptions) -> Self {
        Self {
            agent: ureq::Agent::config_builder()
                .timeout_global(Some(Duration::from_millis(options.timeout_ms)))
                .max_idle_connections(0)
                .max_redirects(0)
                .proxy(None)
                .build()
                .into(),
            base,
            key,
            options,
        }
    }
    fn request(&self, method: &str, path: &str, body: Option<&Value>, limit: u64) -> Result<Value> {
        let url = format!("{}/v0/management/{path}", self.base.trim_end_matches('/'));
        let authorization = format!("Bearer {}", self.key);
        let response = match method {
            "GET" => self
                .agent
                .get(&url)
                .header("Authorization", &authorization)
                .call(),
            "POST" => self
                .agent
                .post(&url)
                .header("Authorization", &authorization)
                .send_json(body.ok_or("missing request body")?),
            "PATCH" => self
                .agent
                .patch(&url)
                .header("Authorization", &authorization)
                .send_json(body.ok_or("missing request body")?),
            _ => return Err("unsupported operation"),
        };
        let mut response = response.map_err(|_| "CPA request failed")?;
        response
            .body_mut()
            .with_config()
            .limit(limit)
            .read_json()
            .map_err(|_| "invalid or oversized CPA response")
    }

    fn target(&self, kind: &str, prefix: &str) -> Result<(Value, Value)> {
        let data = retry(self.options, || {
            self.request("GET", kind, None, MANAGEMENT_LIMIT)
        })?;
        Self::select_target(data, kind, prefix)
    }

    fn target_once(&self, kind: &str, prefix: &str) -> Result<(Value, Value)> {
        Self::select_target(
            self.request("GET", kind, None, MANAGEMENT_LIMIT)?,
            kind,
            prefix,
        )
    }

    fn select_target(data: Value, kind: &str, prefix: &str) -> Result<(Value, Value)> {
        let entries = data
            .get(kind)
            .and_then(Value::as_array)
            .ok_or("invalid discovery response")?;
        let selected: Vec<_> = entries
            .iter()
            .filter(|entry| entry["prefix"].as_str() == Some(prefix))
            .collect();
        if selected.len() != 1 {
            return Err("channel identity missing or ambiguous");
        }
        let entry = selected[0];
        let identity_field = if kind == "openai-compatibility" {
            "name"
        } else {
            "api-key"
        };
        let identity = nonempty(&entry[identity_field])?;
        if entries
            .iter()
            .filter(|v| v[identity_field].as_str() == Some(identity))
            .count()
            != 1
        {
            return Err("write selector is ambiguous");
        }
        let selector = if kind == "openai-compatibility" {
            json!({"name":identity})
        } else {
            json!({"match":identity})
        };
        Ok((entry.clone(), selector))
    }
}

fn pause_before_retry(options: &RequestOptions, attempt: u32) {
    if attempt > 0 {
        std::thread::sleep(Duration::from_millis(
            options.retry_delay_ms * (1u64 << (attempt - 1)),
        ));
    }
}

fn retry<T>(options: &RequestOptions, mut operation: impl FnMut() -> Result<T>) -> Result<T> {
    let mut last = "operation failed";
    for attempt in 0..=options.retries {
        pause_before_retry(options, attempt);
        match operation() {
            Ok(value) => return Ok(value),
            Err(error) => last = error,
        }
    }
    Err(last)
}

fn nonempty(value: &Value) -> Result<&str> {
    value
        .as_str()
        .filter(|s| !s.is_empty())
        .ok_or("required string missing")
}

fn canonical_models(value: &Value) -> Result<Vec<Value>> {
    let mut models = if value.is_null() {
        Vec::new()
    } else {
        value.as_array().ok_or("invalid current models")?.clone()
    };
    for model in &mut models {
        let name = nonempty(&model["name"])?.to_owned();
        let object = model.as_object_mut().ok_or("invalid current model")?;
        // CPA may omit a same-name alias; retain all other fields for full comparison.
        if object
            .get("alias")
            .is_none_or(|v| v.is_null() || v.as_str() == Some(""))
        {
            object.insert("alias".into(), Value::String(name));
        }
    }
    // Only outer order is irrelevant; duplicate counts and nested array order survive.
    models.sort_by_cached_key(Value::to_string);
    Ok(models)
}

fn inventory(
    cpa: &Cpa<'_>,
    kind: &str,
    entry: &Value,
    client_version: &str,
    path_override: Option<&str>,
) -> Result<Vec<String>> {
    let credential = if kind == "openai-compatibility" {
        entry["api-key-entries"]
            .as_array()
            .and_then(|keys| keys.first())
            .unwrap_or(entry)
    } else {
        entry
    };
    let index = credential["auth-index"].as_str().filter(|s| !s.is_empty());
    let token = if index.is_some() {
        "$TOKEN$"
    } else {
        nonempty(&credential["api-key"])?
    };
    let mut headers = entry["headers"].as_object().cloned().unwrap_or_default();
    if kind == "claude-api-key" {
        headers.insert("x-api-key".into(), json!(token));
        headers.insert("anthropic-version".into(), json!("2023-06-01"));
    } else {
        headers.insert("Authorization".into(), json!(format!("Bearer {token}")));
    }
    let base = nonempty(&entry["base-url"])?.trim_end_matches('/');
    let mut path = "/v1/models".to_owned();
    if kind == "openai-compatibility" || kind == "codex-api-key" {
        path.push_str("?client_version=");
        path.push_str(&utf8_percent_encode(client_version, NON_ALPHANUMERIC).to_string());
    }
    if let Some(value) = path_override.filter(|s| !s.is_empty()) {
        path = format!("/{}", value.trim_start_matches('/'));
    }
    // Preserve the existing Go path override and version-prefix joining semantics.
    if base.ends_with("/v1") && path.starts_with("/v1/") {
        path = path.trim_start_matches("/v1").to_owned();
    }
    let url = format!("{base}{path}");
    let mut models = Vec::new();
    let mut cursors = std::collections::HashSet::new();
    let mut cursor: Option<String> = None;
    let mut total_bytes = 0;
    for _ in 0..128 {
        let page_url = match &cursor {
            Some(cursor) => format!(
                "{url}{}after_id={}",
                if url.contains('?') { "&" } else { "?" },
                utf8_percent_encode(cursor, NON_ALPHANUMERIC)
            ),
            None => url.clone(),
        };
        let mut request = json!({"method":"GET", "url":page_url, "header":headers});
        if let Some(index) = index {
            request["auth_index"] = json!(index);
        }
        if let Some(proxy) = credential["proxy-url"]
            .as_str()
            .or_else(|| entry["proxy-url"].as_str())
            .filter(|s| !s.is_empty())
        {
            request["proxy_url"] = json!(proxy);
        }
        let response = cpa.request("POST", "api-call", Some(&request), INVENTORY_LIMIT)?;
        if !response["status_code"]
            .as_u64()
            .is_some_and(|s| (200..300).contains(&s))
        {
            return Err("upstream request failed");
        }
        let body = nonempty(&response["body"])?;
        total_bytes += body.len() as u64;
        if total_bytes > INVENTORY_LIMIT {
            return Err("inventory exceeds limit");
        }
        let data: Value = serde_json::from_str(body).map_err(|_| "invalid upstream JSON")?;
        let rows = data
            .get("data")
            .or_else(|| data.get("models"))
            .and_then(Value::as_array)
            .ok_or("invalid upstream inventory")?;
        for row in rows {
            let id = row
                .get("id")
                .or_else(|| row.get("slug"))
                .or_else(|| row.get("name"))
                .ok_or("model ID missing")?;
            models.push(nonempty(id)?.to_owned());
        }
        match data.get("has_more") {
            None | Some(Value::Bool(false)) => {
                if models.is_empty() {
                    return Err("empty upstream inventory");
                }
                return Ok(models);
            }
            Some(Value::Bool(true)) if kind == "claude-api-key" => {
                let next = nonempty(&data["last_id"])?.to_owned();
                if !cursors.insert(next.clone()) {
                    return Err("repeated pagination cursor");
                }
                cursor = Some(next);
            }
            _ => return Err("invalid pagination response"),
        }
    }
    Err("inventory page limit exceeded")
}

/// Discovery retains invalid rows as channel-local failures rather than hiding them.
pub fn discover_prefixes_with_options(
    cpa_url: &str,
    management_key: &str,
    kind: &str,
    options: &RequestOptions,
) -> Result<Vec<Result<String>>> {
    if !MANAGED_KINDS.contains(&kind) {
        return Err("unmanaged channel kind");
    }
    options.validate()?;
    let cpa = Cpa::new(cpa_url, management_key, options);
    let data = retry(options, || cpa.request("GET", kind, None, MANAGEMENT_LIMIT))?;
    let rows = data
        .get(kind)
        .and_then(Value::as_array)
        .ok_or("invalid discovery response")?;
    Ok(rows
        .iter()
        .map(|row| nonempty(&row["prefix"]).map(str::to_owned))
        .collect())
}

pub fn discover_prefixes(
    cpa_url: &str,
    management_key: &str,
    kind: &str,
) -> Result<Vec<Result<String>>> {
    discover_prefixes_with_options(cpa_url, management_key, kind, &RequestOptions::default())
}

pub fn sync_channel_with_options(
    cpa_url: &str,
    management_key: &str,
    kind: &str,
    prefix: &str,
    client_version: &str,
    filter: &Filter,
    options: &RequestOptions,
) -> Result<Outcome> {
    if !MANAGED_KINDS.contains(&kind) {
        return Err("unmanaged channel kind");
    }
    let include = filter
        .include
        .iter()
        .map(|s| Regex::new(s).map_err(|_| "invalid include regex"))
        .collect::<Result<Vec<_>>>()?;
    let exclude = filter
        .exclude
        .iter()
        .map(|s| Regex::new(s).map_err(|_| "invalid exclude regex"))
        .collect::<Result<Vec<_>>>()?;
    options.validate()?;
    let cpa = Cpa::new(cpa_url, management_key, options);
    let (entry, selector) = cpa.target(kind, prefix)?;
    let desired: Vec<Value> = retry(options, || {
        inventory(&cpa, kind, &entry, client_version, filter.path.as_deref())
    })?
    .into_iter()
    .filter(|id| {
        (include.is_empty() || include.iter().any(|r| r.is_match(id)))
            && !exclude.iter().any(|r| r.is_match(id))
    })
    .map(|id| json!({"name":id,"alias":id}))
    .collect();
    let desired = canonical_models(&json!(desired))?;
    let _writer = CPA_WRITER.lock().map_err(|_| "CPA writer unavailable")?;
    let (current, current_selector) = cpa.target(kind, prefix)?;
    if current_selector != selector {
        return Err("channel identity changed");
    }
    if canonical_models(&current["models"])? == desired {
        return Ok(Outcome::Unchanged);
    }
    let mut patch = current_selector;
    patch["value"] = json!({"models":desired});
    for attempt in 0..=options.retries {
        pause_before_retry(options, attempt);
        if attempt > 0 {
            // A reconciliation read uses this attempt's budget, never a nested retry loop.
            let Ok((current, identity)) = cpa.target_once(kind, prefix) else {
                continue;
            };
            if identity != selector {
                continue;
            }
            let Ok(models) = canonical_models(&current["models"]) else {
                continue;
            };
            if models == desired {
                return Ok(Outcome::Updated);
            }
        }
        if cpa
            .request("PATCH", kind, Some(&patch), MANAGEMENT_LIMIT)
            .is_ok()
        {
            return Ok(Outcome::Updated);
        }
    }
    Ok(Outcome::Unconfirmed)
}

pub fn sync_channel(
    cpa_url: &str,
    management_key: &str,
    kind: &str,
    prefix: &str,
    client_version: &str,
    filter: &Filter,
) -> Result<Outcome> {
    sync_channel_with_options(
        cpa_url,
        management_key,
        kind,
        prefix,
        client_version,
        filter,
        &RequestOptions::default(),
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn comparison_ignores_only_outer_order_and_protocol_defaults() {
        let parse = |text: &str| canonical_models(&serde_json::from_str(text).unwrap()).unwrap();
        assert_eq!(
            parse(r#"[{"name":"A"},{"name":"B","alias":"B"}]"#),
            parse(r#"[ { "alias":"B", "name":"B" }, {"name":"A","alias":"A"} ]"#),
        );
        for (left, right) in [
            (r#"[{"name":"A"},{"name":"A"}]"#, r#"[{"name":"A"}]"#),
            (r#"[{"name":"A","alias":"manual"}]"#, r#"[{"name":"A"}]"#),
            (
                r#"[{"name":"A","modes":[1,2]}]"#,
                r#"[{"name":"A","modes":[2,1]}]"#,
            ),
            (
                r#"[{"name":"A","limit":9007199254740993}]"#,
                r#"[{"name":"A","limit":9007199254740992}]"#,
            ),
            (
                r#"[{"name":"A","limit":"10"}]"#,
                r#"[{"name":"A","limit":10}]"#,
            ),
            (r#"[{"name":"A","extra":null}]"#, r#"[{"name":"A"}]"#),
        ] {
            assert_ne!(parse(left), parse(right));
        }
    }
}
