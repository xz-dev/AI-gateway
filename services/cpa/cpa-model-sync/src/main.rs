use cpa_model_sync::{
    ChannelPreview, Filter, MANAGED_KINDS, Outcome, RequestOptions, current_models_with_options,
    discover_prefixes_with_options, preview_channel_with_options, sync_channel_with_options,
    validate_filter,
};
use serde::Deserialize;
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{
    collections::{BTreeMap, BTreeSet},
    env, fs,
    io::{self, Read},
    process::ExitCode,
    thread,
    time::{Duration, Instant},
};

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Config {
    cpa_url: String,
    client_version: String,
    #[serde(default)]
    channels: BTreeMap<String, Filter>,
    #[serde(default)]
    skip_channels: Vec<String>,
    #[serde(default)]
    requests: RequestOptions,
    #[serde(default = "default_interval")]
    interval_seconds: u64,
}

fn default_interval() -> u64 {
    600
}

fn read_config(path: &std::ffi::OsStr) -> Result<(Vec<u8>, Config), &'static str> {
    let mut bytes = Vec::new();
    if path == "-" {
        io::stdin()
            .read_to_end(&mut bytes)
            .map_err(|_| "cannot read configuration")?;
    } else {
        bytes = fs::read(path).map_err(|_| "cannot read configuration")?;
    }
    let config: Config = serde_json::from_slice(&bytes).map_err(|_| "invalid configuration")?;
    config.requests.validate()?;
    if config.interval_seconds == 0
        || Instant::now()
            .checked_add(Duration::from_secs(config.interval_seconds))
            .is_none()
    {
        return Err("invalid refresh interval");
    }
    Ok((bytes, config))
}

#[derive(Default)]
struct Summary {
    updated: usize,
    unchanged: usize,
    failed: usize,
    unconfirmed: usize,
    skipped: usize,
}

impl Summary {
    fn record(
        &mut self,
        kind: &str,
        prefix: Option<&str>,
        result: Result<Outcome, &'static str>,
        max_attempts: u32,
    ) {
        let (status, reason) = match result {
            Ok(Outcome::Updated) => {
                self.updated += 1;
                ("updated", None)
            }
            Ok(Outcome::Unchanged) => {
                self.unchanged += 1;
                ("unchanged", None)
            }
            Ok(Outcome::Unconfirmed) => {
                self.unconfirmed += 1;
                ("unconfirmed", None)
            }
            Err(reason) => {
                self.failed += 1;
                ("failed", Some(reason))
            }
        };
        // Logical identities and static reasons only; never HTTP records, URLs or keys.
        eprintln!(
            "{}",
            json!({"kind":kind,"prefix":prefix,"status":status,"reason":reason,"max_attempts":max_attempts})
        );
    }
}

fn refresh(config: &Config, key: &str) -> Summary {
    let default_filter = Filter::default();
    let mut summary = Summary::default();
    let mut jobs = Vec::new();
    let max_attempts = config.requests.retries + 1;
    for kind in MANAGED_KINDS {
        match discover_prefixes_with_options(&config.cpa_url, key, kind, &config.requests) {
            Ok(channels) => {
                for channel in channels {
                    match channel {
                        Ok(prefix) if config.skip_channels.contains(&prefix) => {
                            summary.skipped += 1;
                            eprintln!(
                                "{}",
                                json!({"kind":kind,"prefix":prefix,"status":"skipped","reason":"configured skip"})
                            );
                        }
                        Ok(prefix) => jobs.push((kind, prefix)),
                        Err(reason) => summary.record(kind, None, Err(reason), max_attempts),
                    }
                }
            }
            Err(reason) => summary.record(kind, None, Err(reason), max_attempts),
        }
    }
    // Scoped batches bound concurrency and release every job before the next round.
    for batch in jobs.chunks(2) {
        thread::scope(|scope| {
            let handles: Vec<_> = batch
                .iter()
                .map(|(kind, prefix)| {
                    let filter = config.channels.get(prefix).unwrap_or(&default_filter);
                    (
                        *kind,
                        prefix,
                        scope.spawn(move || {
                            sync_channel_with_options(
                                &config.cpa_url,
                                key,
                                kind,
                                prefix,
                                &config.client_version,
                                filter,
                                &config.requests,
                            )
                        }),
                    )
                })
                .collect();
            for (kind, prefix, handle) in handles {
                let result = handle.join().unwrap_or(Err("channel worker failed"));
                summary.record(kind, Some(prefix), result, max_attempts);
            }
        });
    }
    println!(
        "{}",
        json!({"updated":summary.updated,"unchanged":summary.unchanged,"failed":summary.failed,"unconfirmed":summary.unconfirmed,"skipped":summary.skipped})
    );
    summary
}

fn sha256(value: impl AsRef<[u8]>) -> String {
    format!("{:x}", Sha256::digest(value.as_ref()))
}

fn set_digest(ids: &[String]) -> String {
    sha256(serde_json::to_vec(ids).expect("string arrays serialize"))
}

fn difference(left: &[String], right: &[String]) -> Vec<String> {
    let right: BTreeSet<_> = right.iter().collect();
    left.iter()
        .filter(|id| !right.contains(id))
        .cloned()
        .collect()
}

fn preview_value(kind: &str, prefix: &str, skipped: bool, preview: ChannelPreview) -> Value {
    let additions = difference(&preview.desired, &preview.current);
    let removals = difference(&preview.current, &preview.desired);
    let unchanged = preview.current.len() - removals.len();
    let current_digest = set_digest(&preview.current);
    let source_digest = set_digest(&preview.source);
    let desired_digest = set_digest(&preview.desired);
    json!({
        "kind": kind,
        "prefix": prefix,
        "skipped": skipped,
        "current_count": preview.current.len(),
        "source_count": preview.source.len(),
        "desired_count": preview.desired.len(),
        "additions": additions,
        "removals": removals,
        "unchanged": unchanged,
        "current_digest": current_digest,
        "source_digest": source_digest,
        "desired_digest": desired_digest,
    })
}

fn preview(config_bytes: &[u8], config: &Config, key: &str) -> Result<Value, &'static str> {
    let default_filter = Filter::default();
    for filter in config.channels.values() {
        validate_filter(filter)?;
    }
    let mut channels = Vec::new();
    for kind in MANAGED_KINDS {
        for prefix in discover_prefixes_with_options(&config.cpa_url, key, kind, &config.requests)?
        {
            let prefix = prefix?;
            let skipped = config.skip_channels.contains(&prefix);
            let result = if skipped {
                let current = current_models_with_options(
                    &config.cpa_url,
                    key,
                    kind,
                    &prefix,
                    &config.requests,
                )?;
                ChannelPreview {
                    source: current.clone(),
                    desired: current.clone(),
                    current,
                }
            } else {
                let filter = config.channels.get(&prefix).unwrap_or(&default_filter);
                preview_channel_with_options(
                    &config.cpa_url,
                    key,
                    kind,
                    &prefix,
                    &config.client_version,
                    filter,
                    &config.requests,
                )?
            };
            channels.push(preview_value(kind, &prefix, skipped, result));
        }
    }
    channels.sort_by_key(|channel| {
        (
            channel["kind"].as_str().unwrap_or_default().to_owned(),
            channel["prefix"].as_str().unwrap_or_default().to_owned(),
        )
    });
    let current_state_digest = sha256(
        serde_json::to_vec(
            &channels
                .iter()
                .map(|channel| {
                    json!({
                        "kind": channel["kind"],
                        "prefix": channel["prefix"],
                        "digest": channel["current_digest"],
                    })
                })
                .collect::<Vec<_>>(),
        )
        .expect("current state serializes"),
    );
    let source_state_digest = sha256(
        serde_json::to_vec(
            &channels
                .iter()
                .map(|channel| {
                    json!({
                        "kind": channel["kind"],
                        "prefix": channel["prefix"],
                        "digest": channel["source_digest"],
                    })
                })
                .collect::<Vec<_>>(),
        )
        .expect("source state serializes"),
    );
    let desired_state_digest = sha256(
        serde_json::to_vec(
            &channels
                .iter()
                .map(|channel| {
                    json!({
                        "kind": channel["kind"],
                        "prefix": channel["prefix"],
                        "digest": channel["desired_digest"],
                    })
                })
                .collect::<Vec<_>>(),
        )
        .expect("desired state serializes"),
    );
    let image_identity = env::var("CPA_MODEL_SYNC_IMAGE_IDENTITY").unwrap_or_default();
    let approval_input = json!({
        "version": 1,
        "policy_digest": sha256(config_bytes),
        "image_identity": image_identity,
        "current_state_digest": current_state_digest,
        "source_state_digest": source_state_digest,
        "desired_state_digest": desired_state_digest,
        "channels": channels.iter().map(|channel| json!({
            "kind": channel["kind"],
            "prefix": channel["prefix"],
            "skipped": channel["skipped"],
            "current_digest": channel["current_digest"],
            "source_digest": channel["source_digest"],
            "desired_digest": channel["desired_digest"],
        })).collect::<Vec<_>>(),
    });
    let approval_digest = sha256(serde_json::to_vec(&approval_input).expect("preview serializes"));
    Ok(json!({
        "version": 1,
        "policy_digest": approval_input["policy_digest"],
        "image_identity": approval_input["image_identity"],
        "current_state_digest": approval_input["current_state_digest"],
        "source_state_digest": approval_input["source_state_digest"],
        "desired_state_digest": approval_input["desired_state_digest"],
        "approval_digest": approval_digest,
        "channels": channels,
    }))
}

// The interval is whole seconds. Pick a strictly future tick without queuing missed ticks.
fn next_delay(elapsed: Duration, interval_seconds: u64) -> Duration {
    Duration::from_secs(interval_seconds - elapsed.as_secs() % interval_seconds)
        - Duration::from_nanos(u64::from(elapsed.subsec_nanos()))
}

fn run() -> Result<bool, &'static str> {
    let args: Vec<_> = env::args_os().skip(1).collect();
    let (mode, path) = match args.as_slice() {
        [path] => ("daemon", path),
        [flag, path] if flag == "--once" => ("once", path),
        [flag, path] if flag == "--preview" => ("preview", path),
        _ => return Err("usage: cpa-model-sync [--once|--preview] CONFIG.json"),
    };
    let (bytes, config) = read_config(path)?;
    let key = env::var("CPA_MANAGEMENT_KEY").map_err(|_| "CPA_MANAGEMENT_KEY is required")?;
    if key.is_empty() {
        return Err("CPA_MANAGEMENT_KEY is empty");
    }
    if mode == "preview" {
        println!("{}", preview(&bytes, &config, &key)?);
        return Ok(true);
    }
    let start = Instant::now();
    loop {
        let summary = refresh(&config, &key);
        if mode == "once" {
            return Ok(summary.failed == 0 && summary.unconfirmed == 0);
        }
        thread::sleep(next_delay(start.elapsed(), config.interval_seconds));
    }
}

fn main() -> ExitCode {
    match run() {
        Ok(true) => ExitCode::SUCCESS,
        Ok(false) => ExitCode::FAILURE,
        Err(message) => {
            eprintln!("{message}");
            ExitCode::from(2)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn slow_rounds_skip_missed_ticks_without_catch_up() {
        assert_eq!(default_interval(), 600);
        for (elapsed_ms, delay_ms) in [
            (0, 600_000),
            (10, 599_990),
            (600_000, 600_000),
            (601_000, 599_000),
            (1_250_250, 549_750),
        ] {
            assert_eq!(
                next_delay(Duration::from_millis(elapsed_ms), 600),
                Duration::from_millis(delay_ms)
            );
        }
    }
}
