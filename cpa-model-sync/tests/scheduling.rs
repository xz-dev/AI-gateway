use serde_json::{Value, json};
use std::io::{BufRead, BufReader};
use std::process::{Command, Stdio};
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::{Arc, mpsc};
use std::{fs, thread, time::Duration};
use tiny_http::{Response, Server};

#[test]
fn daemon_starts_immediately_and_bounds_slow_channels_to_two() {
    let server = Server::http("127.0.0.1:0").unwrap();
    let url = format!("http://{}", server.server_addr());
    let active = Arc::new(AtomicUsize::new(0));
    let peak = Arc::new(AtomicUsize::new(0));
    let posts = Arc::new(AtomicUsize::new(0));
    let write_active = Arc::new(AtomicUsize::new(0));
    let write_peak = Arc::new(AtomicUsize::new(0));
    let (wa, wp) = (Arc::clone(&write_active), Arc::clone(&write_peak));
    let done = Arc::new(AtomicBool::new(false));
    let (a, p, n, stop) = (
        Arc::clone(&active),
        Arc::clone(&peak),
        Arc::clone(&posts),
        Arc::clone(&done),
    );
    let worker = thread::spawn(move || {
        let mut handlers = Vec::new();
        while !stop.load(Ordering::Relaxed) {
            let Some(request) = server.recv_timeout(Duration::from_millis(10)).unwrap() else {
                continue;
            };
            let (a, p, n) = (Arc::clone(&a), Arc::clone(&p), Arc::clone(&n));
            let (wa, wp) = (Arc::clone(&wa), Arc::clone(&wp));
            handlers.push(thread::spawn(move || {
                let kind = request.url().trim_start_matches("/v0/management/");
                let response = match request.method().as_str() {
                    "GET" if kind == "openai-compatibility" => json!({kind:(0..3).map(|i| json!({
                        "name":format!("p{i}"),"prefix":format!("p{i}"),"base-url":"https://upstream.invalid/v1",
                        "api-key-entries":[{"auth-index":format!("i{i}")}],"models":[{"name":"old","alias":"old"}]
                    })).collect::<Vec<_>>()}),
                    "GET" => json!({kind:[]}),
                    "POST" => {
                        n.fetch_add(1, Ordering::SeqCst);
                        p.fetch_max(a.fetch_add(1, Ordering::SeqCst) + 1, Ordering::SeqCst);
                        thread::sleep(Duration::from_millis(1100));
                        a.fetch_sub(1, Ordering::SeqCst);
                        json!({"status_code":200,"body":r#"{"data":[{"id":"A"}]}"#})
                    }
                    "PATCH" => {
                        wp.fetch_max(wa.fetch_add(1, Ordering::SeqCst) + 1, Ordering::SeqCst);
                        thread::sleep(Duration::from_millis(300));
                        wa.fetch_sub(1, Ordering::SeqCst);
                        json!({"status":"ok"})
                    },
                    _ => panic!("unexpected method"),
                };
                let _ = request.respond(Response::from_string(response.to_string()));
            }));
        }
        for handler in handlers {
            handler.join().unwrap();
        }
    });
    let policy =
        std::env::temp_dir().join(format!("cpa-sync-scheduling-{}.json", std::process::id()));
    fs::write(
        &policy,
        json!({"cpa_url":url,"client_version":"test","requests":{"retries":0,"timeout_ms":5000}})
            .to_string(),
    )
    .unwrap();
    let mut child = Command::new(env!("CARGO_BIN_EXE_cpa-model-sync"))
        .arg(&policy)
        .env("CPA_MANAGEMENT_KEY", "synthetic-management")
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let stdout = child.stdout.take().unwrap();
    let (tx, rx) = mpsc::channel();
    let reader = thread::spawn(move || {
        let _ = tx.send(BufReader::new(stdout).lines().next());
    });
    let first = rx.recv_timeout(Duration::from_secs(10));
    let _ = child.kill();
    let _ = child.wait();
    reader.join().unwrap();
    done.store(true, Ordering::Relaxed);
    worker.join().unwrap();
    fs::remove_file(policy).unwrap();
    let line = first
        .expect("daemon did not refresh on startup")
        .unwrap()
        .unwrap();
    assert_eq!(
        serde_json::from_str::<Value>(&line).unwrap(),
        json!({"updated":3,"unchanged":0,"failed":0,"unconfirmed":0,"skipped":0})
    );
    assert_eq!(peak.load(Ordering::SeqCst), 2);
    assert_eq!(active.load(Ordering::SeqCst), 0);
    assert_eq!(
        write_peak.load(Ordering::SeqCst),
        1,
        "CPA writes must not overlap"
    );
    assert_eq!(write_active.load(Ordering::SeqCst), 0);
    assert_eq!(posts.load(Ordering::SeqCst), 3);
}

#[test]
fn configured_skip_never_fetches_or_writes_the_channel() {
    let server = Server::http("127.0.0.1:0").unwrap();
    let url = format!("http://{}", server.server_addr());
    let done = Arc::new(AtomicBool::new(false));
    let stop = Arc::clone(&done);
    let worker = thread::spawn(move || {
        let mut posts = 0;
        let mut unexpected = 0;
        while !stop.load(Ordering::Relaxed) {
            let Some(mut request) = server.recv_timeout(Duration::from_millis(10)).unwrap() else {
                continue;
            };
            let kind = request
                .url()
                .trim_start_matches("/v0/management/")
                .to_owned();
            let response = match request.method().as_str() {
                "GET" if kind == "claude-api-key" => json!({kind:[
                    {"prefix":"gmicloud","base-url":"https://gmi.invalid/v1/messages","api-key":"gmi-key","disabled":false,"models":[{"name":"existing"}]},
                    {"prefix":"keep","base-url":"https://keep.invalid/v1","api-key":"keep-key","models":[{"name":"A"}]}
                ]}),
                "GET" => json!({kind:[]}),
                "POST" => {
                    let value: Value = serde_json::from_reader(request.as_reader()).unwrap();
                    posts += 1;
                    if value["url"] != "https://keep.invalid/v1/models" {
                        unexpected += 1;
                    }
                    json!({"status_code":200,"body":r#"{"data":[{"id":"A"}]}"#})
                }
                _ => {
                    unexpected += 1;
                    json!({"status":"unexpected write"})
                }
            };
            request
                .respond(Response::from_string(response.to_string()))
                .unwrap();
        }
        (posts, unexpected)
    });
    let policy = std::env::temp_dir().join(format!("cpa-sync-skip-{}.json", std::process::id()));
    fs::write(&policy, json!({"cpa_url":url,"client_version":"test","skip_channels":["gmicloud"],"requests":{"retries":0,"timeout_ms":1000}}).to_string()).unwrap();
    let result = Command::new(env!("CARGO_BIN_EXE_cpa-model-sync"))
        .arg("--once")
        .arg(&policy)
        .env("CPA_MANAGEMENT_KEY", "synthetic-management")
        .output()
        .unwrap();
    done.store(true, Ordering::Relaxed);
    let (posts, unexpected) = worker.join().unwrap();
    fs::remove_file(policy).unwrap();
    assert!(result.status.success());
    assert_eq!((posts, unexpected), (1, 0));
    assert_eq!(
        serde_json::from_slice::<Value>(&result.stdout).unwrap(),
        json!({"updated":0,"unchanged":1,"failed":0,"unconfirmed":0,"skipped":1})
    );
    assert!(
        String::from_utf8(result.stderr)
            .unwrap()
            .lines()
            .any(|line| {
                let v: Value = serde_json::from_str(line).unwrap();
                v["prefix"] == "gmicloud" && v["status"] == "skipped"
            })
    );
}
