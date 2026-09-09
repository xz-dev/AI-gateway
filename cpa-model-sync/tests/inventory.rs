use cpa_model_sync::{Filter, Outcome, RequestOptions, sync_channel_with_options};
use serde_json::{Value, json};
use std::collections::VecDeque;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::{thread, time::Duration};
use tiny_http::{Response, Server};

fn page(data: Value) -> Value {
    json!({"status_code":200, "body":data.to_string()})
}

struct Case<'a> {
    name: &'static str,
    kind: &'static str,
    pages: Vec<Value>,
    include: Vec<&'a str>,
    exclude: Vec<&'a str>,
    expected: Result<Vec<&'static str>, &'static str>,
}

#[test]
fn complete_inventory_and_filter_contracts() {
    let policy: Value = serde_json::from_str(include_str!("../config.example.json")).unwrap();
    let nim: Filter = serde_json::from_value(policy["channels"]["nim"].clone()).unwrap();
    let shuai: Filter = serde_json::from_value(policy["channels"]["shuaiapi"].clone()).unwrap();
    let cases = vec![
        Case {
            name: "deployed NIM selection", kind: "openai-compatibility",
            pages: vec![page(json!({"data":[
                {"id":"deepseek-ai/deepseek-v4-flash-0731"},
                {"id":"minimaxai/minimax-m3"}, {"id":"moonshotai/kimi-k3"},
                {"id":"minimaxai/minimax-m3-preview"}, {"id":"nvidia/embedding-model"}
            ]}))],
            include: nim.include.iter().map(String::as_str).collect(),
            exclude: nim.exclude.iter().map(String::as_str).collect(),
            expected: Ok(vec!["deepseek-ai/deepseek-v4-flash-0731", "minimaxai/minimax-m3", "moonshotai/kimi-k3"]),
        },
        Case {
            name: "deployed ShuaiAPI Claude selection", kind: "openai-compatibility",
            pages: vec![page(json!({"data":[
                {"id":"claude-fable-5-1"}, {"id":"claude-opus-5"},
                {"id":"gpt-5.6-luna"}, {"id":"not-claude-opus-5"}
            ]}))],
            include: shuai.include.iter().map(String::as_str).collect(),
            exclude: shuai.exclude.iter().map(String::as_str).collect(),
            expected: Ok(vec!["claude-fable-5-1", "claude-opus-5"]),
        },
        Case {
            name: "xai native path", kind: "xai-api-key",
            pages: vec![page(json!({"data":[{"id":"A"}]}))],
            include: vec![], exclude: vec![], expected: Ok(vec!["A"]),
        },
        Case {
            name: "vertex compatibility path", kind: "vertex-api-key",
            pages: vec![page(json!({"data":[{"id":"A"}]}))],
            include: vec![], exclude: vec![], expected: Ok(vec!["A"]),
        },
        Case {
            name: "response limit", kind: "openai-compatibility",
            pages: vec![json!({"status_code":200,"body":"x".repeat(33 * 1024 * 1024)})],
            include: vec![], exclude: vec![], expected: Err("invalid or oversized CPA response"),
        },
        Case {
            name: "page limit", kind: "claude-api-key",
            pages: (0..128).map(|i| page(json!({"data":[{"id":format!("model-{i}")}],"has_more":true,"last_id":i.to_string()}))).collect(),
            include: vec![], exclude: vec![], expected: Err("inventory page limit exceeded"),
        },
        Case {
            name: "claude pages",
            kind: "claude-api-key",
            pages: vec![
                page(json!({"data":[{"id":"A"}],"has_more":true,"last_id":"A"})),
                page(json!({"data":[{"id":"B"}],"has_more":false})),
            ],
            include: vec![],
            exclude: vec![],
            expected: Ok(vec!["A", "B"]),
        },
        Case {
            name: "failed second page",
            kind: "claude-api-key",
            pages: vec![
                page(json!({"data":[{"id":"A"}],"has_more":true,"last_id":"A"})),
                json!({"status_code":503,"body":"unavailable"}),
            ],
            include: vec![],
            exclude: vec![],
            expected: Err("upstream request failed"),
        },
        Case {
            name: "cursor cycle",
            kind: "claude-api-key",
            pages: vec![
                page(json!({"data":[{"id":"A"}],"has_more":true,"last_id":"A"})),
                page(json!({"data":[{"id":"B"}],"has_more":true,"last_id":"A"})),
            ],
            include: vec![],
            exclude: vec![],
            expected: Err("repeated pagination cursor"),
        },
        Case {
            name: "raw empty",
            kind: "openai-compatibility",
            pages: vec![page(json!({"data":[]}))],
            include: vec![],
            exclude: vec![],
            expected: Err("empty upstream inventory"),
        },
        Case {
            name: "filtered empty",
            kind: "openai-compatibility",
            pages: vec![page(json!({"data":[{"id":"A"}]}))],
            include: vec![],
            exclude: vec![".*"],
            expected: Ok(vec![]),
        },
        Case {
            name: "exclude wins",
            kind: "openai-compatibility",
            pages: vec![page(
                json!({"data":[{"id":"A:preview"},{"id":"A"},{"id":"B"}]}),
            )],
            include: vec!["^A"],
            exclude: vec![":preview$"],
            expected: Ok(vec!["A"]),
        },
        Case {
            name: "invalid regex",
            kind: "openai-compatibility",
            pages: vec![],
            include: vec!["["],
            exclude: vec![],
            expected: Err("invalid include regex"),
        },
        Case {
            name: "invalid record",
            kind: "openai-compatibility",
            pages: vec![page(json!({"data":[{"id":"A"},{}]}))],
            include: vec![],
            exclude: vec![],
            expected: Err("model ID missing"),
        },
        Case {
            name: "error envelope",
            kind: "openai-compatibility",
            pages: vec![page(json!({"error":"failed"}))],
            include: vec![],
            exclude: vec![],
            expected: Err("invalid upstream inventory"),
        },
        Case {
            name: "codex envelope",
            kind: "codex-api-key",
            pages: vec![page(json!({"models":[{"slug":"A"}]}))],
            include: vec![],
            exclude: vec![],
            expected: Ok(vec!["A"]),
        },
    ];
    for case in cases {
        let server = Server::http("127.0.0.1:0").unwrap();
        let url = format!("http://{}", server.server_addr());
        let original = json!([{"name":"old","alias":"old"}]);
        let state = Arc::new(Mutex::new((original.clone(), 0usize, Vec::new())));
        let shared = Arc::clone(&state);
        let done = Arc::new(AtomicBool::new(false));
        let stop = Arc::clone(&done);
        let kind = case.kind;
        let mut pages: VecDeque<_> = case.pages.into();
        let worker = thread::spawn(move || {
            while !stop.load(Ordering::Relaxed) {
                let Some(mut request) = server.recv_timeout(Duration::from_millis(10)).unwrap()
                else {
                    continue;
                };
                let mut body = String::new();
                request.as_reader().read_to_string(&mut body).unwrap();
                let mut state = shared.lock().unwrap();
                let response = match request.method().as_str() {
                    "GET" => {
                        let mut entry = json!({"name":"fixture","prefix":"fixture","base-url":"https://upstream.invalid/v1","models":state.0});
                        if kind == "openai-compatibility" {
                            entry["api-key-entries"] = json!([{"auth-index":"fixture-index"}]);
                        } else {
                            entry["api-key"] = json!("synthetic-upstream");
                            entry["auth-index"] = json!("fixture-index");
                        }
                        json!({kind:[entry]})
                    }
                    "POST" => {
                        state.2.push(serde_json::from_str::<Value>(&body).unwrap());
                        pages
                            .pop_front()
                            .unwrap_or(json!({"status_code":500,"body":"unexpected page"}))
                    }
                    "PATCH" => {
                        let patch: Value = serde_json::from_str(&body).unwrap();
                        state.0 = patch["value"]["models"].clone();
                        state.1 += 1;
                        json!({"status":"ok"})
                    }
                    _ => json!({"error":"unexpected request"}),
                };
                drop(state);
                let _ = request.respond(Response::from_string(response.to_string()));
            }
        });
        let filter = Filter {
            include: case.include.into_iter().map(str::to_owned).collect(),
            exclude: case.exclude.into_iter().map(str::to_owned).collect(),
            ..Filter::default()
        };
        // This table isolates parsing/filtering; retry budgets are tested in retries.rs.
        let options = RequestOptions {
            retries: 0,
            ..RequestOptions::default()
        };
        let outcome = sync_channel_with_options(
            &url,
            "synthetic-management",
            kind,
            "fixture",
            "test",
            &filter,
            &options,
        );
        done.store(true, Ordering::Relaxed);
        worker.join().unwrap();
        let state = state.lock().unwrap();
        match case.expected {
            Ok(ids) => {
                assert_eq!(outcome, Ok(Outcome::Updated), "{}", case.name);
                assert_eq!(state.1, 1, "{}", case.name);
                let models: Vec<Value> =
                    ids.iter().map(|id| json!({"name":id,"alias":id})).collect();
                assert_eq!(state.0, json!(models), "{}", case.name);
            }
            Err(error) => {
                assert_eq!(outcome, Err(error), "{}", case.name);
                assert_eq!(state.1, 0, "{}", case.name);
                assert_eq!(state.0, original, "{}", case.name);
            }
        }
        for (index, forwarded) in state.2.iter().enumerate() {
            assert_eq!(forwarded["auth_index"], "fixture-index");
            if kind == "claude-api-key" {
                assert_eq!(forwarded["header"]["x-api-key"], "$TOKEN$");
                let expected = if index == 0 {
                    "https://upstream.invalid/v1/models".to_owned()
                } else if case.name == "page limit" {
                    format!("https://upstream.invalid/v1/models?after_id={}", index - 1)
                } else {
                    "https://upstream.invalid/v1/models?after_id=A".to_owned()
                };
                assert_eq!(forwarded["url"], expected);
            } else {
                assert_eq!(forwarded["header"]["Authorization"], "Bearer $TOKEN$");
                let expected = if kind == "xai-api-key" || kind == "vertex-api-key" {
                    "https://upstream.invalid/v1/models"
                } else {
                    "https://upstream.invalid/v1/models?client_version=test"
                };
                assert_eq!(forwarded["url"], expected);
            }
        }
    }
}
