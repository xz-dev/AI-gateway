use cpa_model_sync::{Filter, Outcome, sync_channel};
use serde_json::{Value, json};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;
use tiny_http::{Header, Response, Server};

struct State {
    models: Value,
    writes: usize,
    failure: Option<String>,
}

#[test]
fn complete_replacement_then_zero_writes_from_current_cpa_state() {
    let server = Server::http("127.0.0.1:0").unwrap();
    let url = format!("http://{}", server.server_addr());
    let state = Arc::new(Mutex::new(State {
        models: json!([{"name":"old", "alias":"old"}]),
        writes: 0,
        failure: None,
    }));
    let done = Arc::new(AtomicBool::new(false));
    let shared = Arc::clone(&state);
    let stop = Arc::clone(&done);
    let worker = thread::spawn(move || {
        while !stop.load(Ordering::Relaxed) {
            let Some(mut request) = server.recv_timeout(Duration::from_millis(10)).unwrap() else {
                continue;
            };
            let authorized = request.headers().iter().any(|h| {
                h.field.equiv("Authorization") && h.value.as_str() == "Bearer synthetic-management"
            });
            let mut body = String::new();
            request.as_reader().read_to_string(&mut body).unwrap();
            let mut state = shared.lock().unwrap();
            let output = match (request.method().as_str(), request.url()) {
                ("GET", "/v0/management/openai-compatibility") => json!({
                    "openai-compatibility":[{
                        "name":"fixture", "prefix":"fixture", "disabled":true,
                        "base-url":"https://upstream.invalid/v1",
                        "api-key-entries":[{"api-key":"synthetic-upstream"}],
                        "models":state.models
                    }]
                }),
                ("POST", "/v0/management/api-call") => {
                    let value: Value = serde_json::from_str(&body).unwrap();
                    if value["method"] != "GET"
                        || value["url"] != "https://upstream.invalid/v1/special?client_version=test"
                        || value["header"]["Authorization"] != "Bearer synthetic-upstream"
                    {
                        state.failure =
                            Some("upstream discovery did not use CPA forwarding".into());
                    }
                    json!({"status_code":200,"body":r#"{"data":[{"id":"A"},{"id":"B"}]}"#})
                }
                ("PATCH", "/v0/management/openai-compatibility") => {
                    let value: Value = serde_json::from_str(&body).unwrap();
                    if value
                        != json!({"name":"fixture","value":{"models":[{"name":"A","alias":"A"}]}})
                    {
                        state.failure =
                            Some("write was not one complete models-only replacement".into());
                    }
                    state.models = value["value"]["models"].clone();
                    state.writes += 1;
                    json!({"status":"ok"})
                }
                _ => {
                    state.failure = Some("unexpected external operation".into());
                    json!({"error":"unexpected operation"})
                }
            };
            if !authorized {
                state.failure = Some("management authorization missing".into());
            }
            drop(state);
            let response = Response::from_string(output.to_string())
                .with_header(Header::from_bytes("Content-Type", "application/json").unwrap());
            request.respond(response).unwrap();
        }
    });
    let filter = Filter {
        include: vec!["^A$".into()],
        exclude: vec![],
        path: Some("/v1/special?client_version=test".into()),
    };
    let first = sync_channel(
        &url,
        "synthetic-management",
        "openai-compatibility",
        "fixture",
        "test",
        &filter,
    );
    let writes_after_first = state.lock().unwrap().writes;
    // A new invocation must compare CPA, not depend on a previous in-memory snapshot.
    let second = sync_channel(
        &url,
        "synthetic-management",
        "openai-compatibility",
        "fixture",
        "test",
        &filter,
    );
    done.store(true, Ordering::Relaxed);
    worker.join().unwrap();
    let state = state.lock().unwrap();
    assert_eq!(first, Ok(Outcome::Updated));
    assert_eq!(writes_after_first, 1);
    assert_eq!(second, Ok(Outcome::Unchanged));
    assert_eq!(state.writes, 1);
    assert_eq!(state.models, json!([{"name":"A","alias":"A"}]));
    assert_eq!(state.failure, None);
}
