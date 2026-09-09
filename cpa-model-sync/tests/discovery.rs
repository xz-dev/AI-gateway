use cpa_model_sync::{Filter, MANAGED_KINDS, discover_prefixes, sync_channel};
use serde_json::json;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::{thread, time::Duration};
use tiny_http::{Response, Server};

#[test]
fn discovery_keeps_disabled_and_invalid_rows_but_never_contacts_unmanaged_kinds() {
    let server = Server::http("127.0.0.1:0").unwrap();
    let url = format!("http://{}", server.server_addr());
    let requests = Arc::new(Mutex::new(Vec::new()));
    let seen = Arc::clone(&requests);
    let done = Arc::new(AtomicBool::new(false));
    let stop = Arc::clone(&done);
    let worker = thread::spawn(move || {
        while !stop.load(Ordering::Relaxed) {
            let Some(request) = server.recv_timeout(Duration::from_millis(10)).unwrap() else {
                continue;
            };
            let kind = request.url().trim_start_matches("/v0/management/");
            seen.lock()
                .unwrap()
                .push((request.method().as_str().to_owned(), kind.to_owned()));
            let data = json!({kind:[
                {"prefix":"enabled","disabled":false},
                {"prefix":"disabled","disabled":true},
                {"disabled":true}
            ]});
            request
                .respond(Response::from_string(data.to_string()))
                .unwrap();
        }
    });
    for kind in MANAGED_KINDS {
        assert_eq!(
            discover_prefixes(&url, "synthetic-management", kind).unwrap(),
            vec![
                Ok("enabled".to_owned()),
                Ok("disabled".to_owned()),
                Err("required string missing"),
            ]
        );
    }
    for kind in ["gemini-api-key", "interactions-api-key", "auth-files"] {
        assert!(discover_prefixes(&url, "synthetic-management", kind).is_err());
        assert!(
            sync_channel(
                &url,
                "synthetic-management",
                kind,
                "unused",
                "test",
                &Filter::default()
            )
            .is_err()
        );
    }
    done.store(true, Ordering::Relaxed);
    worker.join().unwrap();
    let requests = requests.lock().unwrap();
    assert_eq!(requests.len(), 5);
    assert!(
        requests
            .iter()
            .all(|(method, kind)| method == "GET" && MANAGED_KINDS.contains(&kind.as_str()))
    );
}

#[test]
fn ambiguous_or_missing_identity_stops_before_fetch_or_write() {
    let server = Server::http("127.0.0.1:0").unwrap();
    let url = format!("http://{}", server.server_addr());
    let requests = Arc::new(Mutex::new(Vec::new()));
    let seen = Arc::clone(&requests);
    let done = Arc::new(AtomicBool::new(false));
    let stop = Arc::clone(&done);
    let worker = thread::spawn(move || {
        while !stop.load(Ordering::Relaxed) {
            let Some(request) = server.recv_timeout(Duration::from_millis(10)).unwrap() else {
                continue;
            };
            let kind = request.url().trim_start_matches("/v0/management/");
            seen.lock()
                .unwrap()
                .push(request.method().as_str().to_owned());
            let identity = if kind == "openai-compatibility" {
                "name"
            } else {
                "api-key"
            };
            let data = json!({kind:[
                {"prefix":"same-prefix",identity:"synthetic-one"},
                {"prefix":"same-prefix",identity:"synthetic-two"},
                {"prefix":"unique-prefix",identity:"synthetic-one"},
                {"prefix":"no-selector"}
            ]});
            request
                .respond(Response::from_string(data.to_string()))
                .unwrap();
        }
    });
    for kind in MANAGED_KINDS {
        for prefix in [
            "same-prefix",
            "unique-prefix",
            "no-selector",
            "missing-prefix",
        ] {
            let error = sync_channel(
                &url,
                "synthetic-management",
                kind,
                prefix,
                "test",
                &Filter::default(),
            )
            .unwrap_err();
            assert!(!error.contains("synthetic-"));
        }
    }
    done.store(true, Ordering::Relaxed);
    worker.join().unwrap();
    let requests = requests.lock().unwrap();
    assert_eq!(requests.len(), 20);
    assert!(requests.iter().all(|method| method == "GET"));
}
