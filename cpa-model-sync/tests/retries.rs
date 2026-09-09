use cpa_model_sync::{Filter, Outcome, RequestOptions, sync_channel_with_options};
use serde_json::{Value, json};
use std::io::Write;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::{thread, time::Duration};
use tiny_http::{Response, Server};

#[test]
fn retry_and_confirmation_budgets_are_not_nested() {
    for (mode, expected, posts, writes, reads) in [
        ("fetch-fourth", Ok(Outcome::Updated), 4, 1, 2),
        ("fetch-exhausted", Err("upstream request failed"), 4, 0, 1),
        ("page-exhausted", Err("upstream request failed"), 8, 0, 1),
        ("current-unreadable", Err("CPA request failed"), 1, 0, 5),
        ("write-fourth", Ok(Outcome::Updated), 1, 4, 5),
        ("write-exhausted", Ok(Outcome::Unconfirmed), 1, 4, 5),
        ("response-lost", Ok(Outcome::Updated), 1, 1, 3),
        ("unconfirmed", Ok(Outcome::Unconfirmed), 1, 1, 5),
    ] {
        let server = Server::http("127.0.0.1:0").unwrap();
        let url = format!("http://{}", server.server_addr());
        let state = Arc::new(Mutex::new((json!([{"name":"old","alias":"old"}]), 0, 0, 0)));
        let shared = Arc::clone(&state);
        let done = Arc::new(AtomicBool::new(false));
        let stop = Arc::clone(&done);
        let kind = if mode == "page-exhausted" {
            "claude-api-key"
        } else {
            "openai-compatibility"
        };
        let worker = thread::spawn(move || {
            while !stop.load(Ordering::Relaxed) {
                let Some(mut request) = server.recv_timeout(Duration::from_millis(10)).unwrap()
                else {
                    continue;
                };
                let mut text = String::new();
                request.as_reader().read_to_string(&mut text).unwrap();
                let mut state = shared.lock().unwrap();
                let mut status = 200;
                let mut disconnect = false;
                let response = match request.method().as_str() {
                    "GET" => {
                        state.3 += 1;
                        if (mode == "unconfirmed" && state.2 > 0)
                            || (mode == "current-unreadable" && state.1 > 0)
                        {
                            status = 503;
                        }
                        json!({kind:[{"name":"fixture","prefix":"fixture","api-key":"synthetic-upstream","auth-index":"fixture-index",
                            "api-key-entries":[{"auth-index":"fixture-index"}],"base-url":"https://upstream.invalid/v1","models":state.0}]})
                    }
                    "POST" => {
                        state.1 += 1;
                        let body: Value = serde_json::from_str(&text).unwrap();
                        let after = body["url"].as_str().unwrap().contains("after_id=");
                        let failed = mode == "fetch-exhausted"
                            || (mode == "fetch-fourth" && state.1 <= 3)
                            || (mode == "page-exhausted" && after);
                        let data = if mode == "page-exhausted" {
                            json!({"data":[{"id":"A"}],"has_more":true,"last_id":"A"})
                        } else {
                            json!({"data":[{"id":"A"}]})
                        };
                        json!({"status_code":if failed {503} else {200},"body":data.to_string()})
                    }
                    "PATCH" => {
                        state.2 += 1;
                        if mode == "write-exhausted" || (mode == "write-fourth" && state.2 <= 3) {
                            status = 503;
                        } else {
                            let body: Value = serde_json::from_str(&text).unwrap();
                            state.0 = body["value"]["models"].clone();
                            disconnect = mode == "response-lost" || mode == "unconfirmed";
                        }
                        json!({"status":"ok"})
                    }
                    _ => panic!("unexpected method"),
                };
                drop(state);
                if disconnect {
                    // Commit state, then lose the response body before the client can confirm it.
                    let mut writer = request.into_writer();
                    let _ = writer.write_all(
                        b"HTTP/1.1 200 OK\r\nContent-Length: 99\r\nConnection: close\r\n\r\n",
                    );
                } else {
                    let _ = request.respond(
                        Response::from_string(response.to_string()).with_status_code(status),
                    );
                }
            }
        });
        let options = RequestOptions {
            retry_delay_ms: 0,
            timeout_ms: 200,
            ..RequestOptions::default()
        };
        let outcome = sync_channel_with_options(
            &url,
            "synthetic-management",
            kind,
            "fixture",
            "test",
            &Filter::default(),
            &options,
        );
        done.store(true, Ordering::Relaxed);
        worker.join().unwrap();
        let state = state.lock().unwrap();
        assert_eq!(outcome, expected, "{mode}");
        assert_eq!(
            (state.1, state.2, state.3),
            (posts, writes, reads),
            "{mode}"
        );
        if writes == 0 {
            assert_eq!(state.0, json!([{"name":"old","alias":"old"}]), "{mode}");
        }
    }
}
