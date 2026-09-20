use cpa_model_sync::{Filter, Outcome, sync_channel};
use serde_json::{Value, json};
use std::io::Write;
use std::process::{Command, Output, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::{thread, time::Duration};
use tiny_http::{Response, Server};

#[derive(Default)]
struct State {
    models: Value,
    requests: usize,
    source_reads: usize,
    writes: usize,
}

fn run_preview(config: &Value) -> Output {
    let mut child = Command::new(env!("CARGO_BIN_EXE_cpa-model-sync"))
        .args(["--preview", "-"])
        .env("CPA_MANAGEMENT_KEY", "synthetic-management")
        .env("CPA_MODEL_SYNC_IMAGE_IDENTITY", "fixture-image")
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    child
        .stdin
        .take()
        .unwrap()
        .write_all(config.to_string().as_bytes())
        .unwrap();
    child.wait_with_output().unwrap()
}

#[test]
fn preview_is_stable_read_only_and_matches_sync_filtering() {
    let server = Server::http("127.0.0.1:0").unwrap();
    let url = format!("http://{}", server.server_addr());
    let state = Arc::new(Mutex::new(State {
        models: json!([{"name":"old","alias":"old"}]),
        ..State::default()
    }));
    let shared = Arc::clone(&state);
    let done = Arc::new(AtomicBool::new(false));
    let stop = Arc::clone(&done);
    let worker = thread::spawn(move || {
        while !stop.load(Ordering::Relaxed) {
            let Some(mut request) = server.recv_timeout(Duration::from_millis(10)).unwrap() else {
                continue;
            };
            let mut body = String::new();
            request.as_reader().read_to_string(&mut body).unwrap();
            let mut state = shared.lock().unwrap();
            state.requests += 1;
            let response = match (request.method().as_str(), request.url()) {
                ("GET", path) if path.starts_with("/v0/management/") => {
                    let kind = path.trim_start_matches("/v0/management/");
                    if kind == "openai-compatibility" {
                        json!({kind:[{
                            "name":"fixture", "prefix":"fixture",
                            "base-url":"https://upstream.invalid/v1",
                            "api-key-entries":[{"auth-index":"fixture-index"}],
                            "models":state.models
                        }]})
                    } else {
                        json!({kind:[]})
                    }
                }
                ("POST", "/v0/management/api-call") => {
                    state.source_reads += 1;
                    json!({
                        "status_code":200,
                        "body":r#"{"data":[{"id":"B"},{"id":"A"}]}"#
                    })
                }
                ("PATCH", "/v0/management/openai-compatibility") => {
                    let patch: Value = serde_json::from_str(&body).unwrap();
                    state.models = patch["value"]["models"].clone();
                    state.writes += 1;
                    json!({"status":"ok"})
                }
                _ => json!({"error":"unexpected request"}),
            };
            drop(state);
            request
                .respond(Response::from_string(response.to_string()))
                .unwrap();
        }
    });

    let config = json!({
        "cpa_url":url,
        "client_version":"test",
        "interval_seconds":600,
        "requests":{"retries":0,"retry_delay_ms":0,"timeout_ms":1000},
        "channels":{"fixture":{"include":["^A$"],"exclude":[]}}
    });
    let first = run_preview(&config);
    assert!(
        first.status.success(),
        "{}",
        String::from_utf8_lossy(&first.stderr)
    );
    let second = run_preview(&config);
    assert!(
        second.status.success(),
        "{}",
        String::from_utf8_lossy(&second.stderr)
    );
    assert_eq!(first.stdout, second.stdout);
    assert!(!String::from_utf8_lossy(&first.stdout).contains("synthetic-management"));

    let preview: Value = serde_json::from_slice(&first.stdout).unwrap();
    assert_eq!(preview["version"], 1);
    assert_eq!(preview["image_identity"], "fixture-image");
    assert_eq!(preview["approval_digest"].as_str().unwrap().len(), 64);
    for field in [
        "current_state_digest",
        "source_state_digest",
        "desired_state_digest",
    ] {
        assert_eq!(preview[field].as_str().unwrap().len(), 64);
    }
    let channel = &preview["channels"][0];
    assert_eq!(channel["kind"], "openai-compatibility");
    assert_eq!(channel["prefix"], "fixture");
    assert_eq!(channel["current_count"], 1);
    assert_eq!(channel["source_count"], 2);
    assert_eq!(channel["desired_count"], 1);
    assert!(channel.get("current").is_none());
    assert!(channel.get("source").is_none());
    assert!(channel.get("desired").is_none());
    assert_eq!(channel["additions"], json!(["A"]));
    assert_eq!(channel["removals"], json!(["old"]));
    assert_eq!(channel["unchanged"], 0);
    assert_eq!(state.lock().unwrap().writes, 0);

    let outcome = sync_channel(
        &url,
        "synthetic-management",
        "openai-compatibility",
        "fixture",
        "test",
        &Filter {
            include: vec!["^A$".into()],
            ..Filter::default()
        },
    );
    assert_eq!(outcome, Ok(Outcome::Updated));
    assert_eq!(
        state.lock().unwrap().models,
        json!([{"name":"A","alias":"A"}])
    );

    let source_reads_before_skip = state.lock().unwrap().source_reads;
    let skipped = json!({
        "cpa_url":url,
        "client_version":"test",
        "interval_seconds":600,
        "requests":{"retries":0,"retry_delay_ms":0,"timeout_ms":1000},
        "skip_channels":["fixture"]
    });
    let skipped_output = run_preview(&skipped);
    assert!(skipped_output.status.success());
    let skipped_preview: Value = serde_json::from_slice(&skipped_output.stdout).unwrap();
    let skipped_channel = &skipped_preview["channels"][0];
    assert_eq!(skipped_channel["skipped"], true);
    assert_eq!(skipped_channel["additions"], json!([]));
    assert_eq!(skipped_channel["removals"], json!([]));
    assert_eq!(state.lock().unwrap().source_reads, source_reads_before_skip);

    let invalid_limits = json!({
        "cpa_url":url,
        "client_version":"test",
        "requests":{"timeout_ms":0}
    });
    let requests_before_invalid_limits = state.lock().unwrap().requests;
    assert_eq!(run_preview(&invalid_limits).status.code(), Some(2));
    assert_eq!(
        state.lock().unwrap().requests,
        requests_before_invalid_limits
    );

    let requests_before_invalid = state.lock().unwrap().requests;
    let invalid = json!({
        "cpa_url":url,
        "client_version":"test",
        "channels":{"fixture":{"include":["["],"exclude":[]}}
    });
    let invalid_output = run_preview(&invalid);
    assert_eq!(invalid_output.status.code(), Some(2));
    assert!(String::from_utf8_lossy(&invalid_output.stderr).contains("invalid include regex"));
    assert_eq!(state.lock().unwrap().requests, requests_before_invalid);

    let unknown = json!({"cpa_url":url,"client_version":"test","unknown":true});
    assert_eq!(run_preview(&unknown).status.code(), Some(2));

    done.store(true, Ordering::Relaxed);
    worker.join().unwrap();
}
