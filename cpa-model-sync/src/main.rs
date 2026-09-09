use cpa_model_sync::{
    Filter, MANAGED_KINDS, Outcome, RequestOptions, discover_prefixes_with_options,
    sync_channel_with_options,
};
use serde::Deserialize;
use serde_json::json;
use std::{
    collections::BTreeMap,
    env, fs,
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

// The interval is whole seconds. Pick a strictly future tick without queuing missed ticks.
fn next_delay(elapsed: Duration, interval_seconds: u64) -> Duration {
    Duration::from_secs(interval_seconds - elapsed.as_secs() % interval_seconds)
        - Duration::from_nanos(u64::from(elapsed.subsec_nanos()))
}

fn run() -> Result<bool, &'static str> {
    let args: Vec<_> = env::args_os().skip(1).collect();
    let (once, path) = match args.as_slice() {
        [path] => (false, path),
        [flag, path] if flag == "--once" => (true, path),
        _ => return Err("usage: cpa-model-sync [--once] CONFIG.json"),
    };
    let key = env::var("CPA_MANAGEMENT_KEY").map_err(|_| "CPA_MANAGEMENT_KEY is required")?;
    if key.is_empty() {
        return Err("CPA_MANAGEMENT_KEY is empty");
    }
    let bytes = fs::read(path).map_err(|_| "cannot read configuration")?;
    let config: Config = serde_json::from_slice(&bytes).map_err(|_| "invalid configuration")?;
    config.requests.validate()?;
    if config.interval_seconds == 0
        || Instant::now()
            .checked_add(Duration::from_secs(config.interval_seconds))
            .is_none()
    {
        return Err("invalid refresh interval");
    }
    drop(bytes);
    let start = Instant::now();
    loop {
        let summary = refresh(&config, &key);
        if once {
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
