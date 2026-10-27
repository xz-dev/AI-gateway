use cpa_model_sync::{Filter, Outcome, RequestOptions, sync_channel_with_options};
use serde_json::{Value, json};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::{thread, time::Duration};
use tiny_http::{Response, Server};

// CPA sends Claude keys without base-url to Anthropic; inventory must follow the same origin.
#[test]
fn claude_key_without_base_url_uses_cpa_default_origin() {
    for base in [None, Some("")] {
        let server = Server::http("127.0.0.1:0").unwrap();
        let url = format!("http://{}", server.server_addr());
        let forwarded = Arc::new(Mutex::new(Vec::<Value>::new()));
        let seen = Arc::clone(&forwarded);
        let done = Arc::new(AtomicBool::new(false));
        let stop = Arc::clone(&done);
        let worker = thread::spawn(move || {
            while !stop.load(Ordering::Relaxed) {
                let Some(mut request) = server.recv_timeout(Duration::from_millis(10)).unwrap()
                else {
                    continue;
                };
                let mut body = String::new();
                request.as_reader().read_to_string(&mut body).unwrap();
                let response = match request.method().as_str() {
                    "GET" => {
                        let mut entry = json!({"prefix":"fixture","api-key":"synthetic-upstream",
                            "auth-index":"fixture-index","models":[{"name":"old","alias":"old"}]});
                        if let Some(base) = base {
                            entry["base-url"] = json!(base);
                        }
                        json!({"claude-api-key":[entry]})
                    }
                    "POST" => {
                        seen.lock()
                            .unwrap()
                            .push(serde_json::from_str(&body).unwrap());
                        json!({"status_code":200,"body":json!({"data":[{"id":"A"}],"has_more":false}).to_string()})
                    }
                    "PATCH" => json!({"status":"ok"}),
                    _ => json!({"error":"unexpected request"}),
                };
                let _ = request.respond(Response::from_string(response.to_string()));
            }
        });
        let options = RequestOptions {
            retries: 0,
            ..RequestOptions::default()
        };
        let outcome = sync_channel_with_options(
            &url,
            "synthetic-management",
            "claude-api-key",
            "fixture",
            "test",
            &Filter::default(),
            &options,
        );
        done.store(true, Ordering::Relaxed);
        worker.join().unwrap();
        assert_eq!(outcome, Ok(Outcome::Updated), "base-url {base:?}");
        let forwarded = forwarded.lock().unwrap();
        assert_eq!(forwarded.len(), 1);
        assert_eq!(forwarded[0]["url"], "https://api.anthropic.com/v1/models");
        assert_eq!(forwarded[0]["auth_index"], "fixture-index");
    }
}
