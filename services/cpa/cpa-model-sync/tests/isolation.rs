use serde_json::{Value, json};
use std::process::Command;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::{fs, thread, time::Duration};
use tiny_http::{Response, Server};

#[test]
fn failed_channel_does_not_prevent_a_healthy_update() {
    let server = Server::http("127.0.0.1:0").unwrap();
    let url = format!("http://{}", server.server_addr());
    let state = Arc::new(Mutex::new((0, 0, 0)));
    let shared = Arc::clone(&state);
    let done = Arc::new(AtomicBool::new(false));
    let stop = Arc::clone(&done);
    let worker = thread::spawn(move || {
        while !stop.load(Ordering::Relaxed) {
            let Some(mut request) = server.recv_timeout(Duration::from_millis(10)).unwrap() else {
                continue;
            };
            let mut text = String::new();
            request.as_reader().read_to_string(&mut text).unwrap();
            let mut state = shared.lock().unwrap();
            let kind = request.url().trim_start_matches("/v0/management/");
            let response = match request.method().as_str() {
                "GET" if kind == "openai-compatibility" => json!({kind:[
                    {"name":"bad","prefix":"bad","base-url":"https://bad.invalid/v1","api-key-entries":[{"auth-index":"bad-index"}],"models":[{"name":"old","alias":"old"}]},
                    {"name":"good","prefix":"good","base-url":"https://good.invalid/v1","api-key-entries":[{"auth-index":"good-index"}],"models":[{"name":"old","alias":"old"}]}
                ]}),
                "GET" => json!({kind:[]}),
                "POST" => {
                    let body: Value = serde_json::from_str(&text).unwrap();
                    if body["auth_index"] == "bad-index" {
                        state.0 += 1;
                        json!({"status_code":503,"body":"unavailable"})
                    } else {
                        state.1 += 1;
                        json!({"status_code":200,"body":r#"{"data":[{"id":"A"}]}"#})
                    }
                }
                "PATCH" => {
                    let body: Value = serde_json::from_str(&text).unwrap();
                    assert_eq!(
                        body,
                        json!({"name":"good","value":{"models":[{"name":"A","alias":"A"}]}})
                    );
                    state.2 += 1;
                    json!({"status":"ok"})
                }
                _ => panic!("unexpected operation"),
            };
            drop(state);
            let _ = request.respond(Response::from_string(response.to_string()));
        }
    });
    let policy =
        std::env::temp_dir().join(format!("cpa-sync-isolation-{}.json", std::process::id()));
    fs::write(&policy, json!({"cpa_url":url,"client_version":"test","requests":{"retry_delay_ms":0,"timeout_ms":200}}).to_string()).unwrap();
    let output = Command::new(env!("CARGO_BIN_EXE_cpa-model-sync"))
        .args(["--once", policy.to_str().unwrap()])
        .env("CPA_MANAGEMENT_KEY", "synthetic-management")
        .output()
        .unwrap();
    done.store(true, Ordering::Relaxed);
    worker.join().unwrap();
    fs::remove_file(policy).unwrap();
    assert_eq!(output.status.code(), Some(1));
    assert_eq!(*state.lock().unwrap(), (4, 1, 1));
    assert_eq!(
        serde_json::from_slice::<Value>(&output.stdout).unwrap(),
        json!({"updated":1,"unchanged":0,"failed":1,"unconfirmed":0,"skipped":0})
    );
    assert!(!String::from_utf8_lossy(&output.stderr).contains("synthetic-management"));
}
