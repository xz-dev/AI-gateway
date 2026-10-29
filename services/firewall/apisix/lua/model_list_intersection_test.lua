-- 使用已有 APISIX 镜像的真实 JSON codec，HTTP 与 ngx 请求状态仅为本地 fixture。
local home = os.getenv("APISIX_HOME") or "/usr/local/apisix"
package.path = home .. "/?.lua;" .. home .. "/deps/share/lua/5.1/?.lua;" ..
    home .. "/deps/share/lua/5.1/?/init.lua;" .. package.path
package.cpath = home .. "/deps/lib/lua/5.1/?.so;" ..
    home .. "/deps/lib64/lua/5.1/?.so;" .. package.cpath
local cjson = require("cjson.safe")
cjson.decode_array_with_array_mt(true)
local json = require("apisix.core.json")
local real_ngx = ngx
local source = (arg[0]:match("^(.*)/") or ".") .. "/model_list_intersection.lua"
local total = 0

local function run(original, options)
    options = options or {}
    local calls = {}
    local warnings = {}
    local active = options.active or 0
    local clock = {t = 0}
    local dict = {
        incr = function(_, _, delta) active = active + delta; return active end,
        expire = function() return true end,
        set = function(_, _, value) active = value end,
    }
    local query = options.query
    if query == nil and not options.entry then
        query = "client_version=fixture"
    end
    local fake = {
        var = {uri = options.path or "/v1/models", args = query,
            remote_addr = "127.0.0.1", http_if_none_match = options.etag,
            http_if_match = options.if_match},
        req = {get_method = function() return "GET" end,
            get_headers = function() return {Authorization = "Bearer fixture-only"} end},
        header = {}, shared = {["model-list-intersection"] = dict}, md5 = real_ngx.md5,
        now = function() return clock.t end,
        sleep = function(s) clock.t = clock.t + (s or 0) end,
    }
    package.loaded["apisix.core"] = {json = json, log = {error = function() end,
        warn = function(...) warnings[#warnings + 1] = table.concat({...}) end}}
    package.loaded["resty.http"] = {new = function()
        return {
            set_timeouts = function() end,
            connect = function(self, host, port) self.host = host; self.port = port; return true end,
            request = function(self, request)
                calls[#calls + 1] = {host = self.host, port = self.port, request = request, active = active}
                local basic = self.host == "ai-sse-keepalive-ingress-relay"
                local body = basic and (options.basic or '{"models":[{"slug":"c/allowed"}]}') or original
                local headers = basic and (options.basic_headers or {}) or (options.headers or {})
                local status = basic and (options.basic_status or 200) or (options.original_status or 200)
                local sent = false
                return {status = status, headers = headers, body_reader = function()
                    if sent then return nil end
                    sent = true
                    return body
                end}
            end,
            close = function() end, set_keepalive = function() return true end,
        }
    end}
    _G.ngx = fake
    local module = dofile(source)
    local ctx = {}
    local ok, status, body = pcall(module[options.entry or "run"], nil, ctx)
    local held = ctx.model_list_intersection_acquired
    module.release(nil, ctx)
    _G.ngx = real_ngx
    assert(ok, status)
    assert(active == (options.active or 0), "admission lease leaked")
    return status, body, fake.header, calls, warnings, held
end

local function test(name, f)
    f()
    total = total + 1
    print("PASS " .. name)
end

local record = [[{"slug":"c/allowed","id":"c/allowed","vendor":{"sequence":9007199254740993,"huge":1234567890123456789012345678901234567890,"price":0.1234567890123456789012345,"text":"escaped \\\" } , [","array":[null,{"n":9007199254740993},false]},"input_modalities":["text","image"],"output_modalities":["text"]}]]
local denied = [[{"slug":"c/denied","output_modalities":["image"]}]]
local original = '{"models":[' .. denied .. ',' .. record .. '],"revision":9007199254740993}'
local expected = '{"models":[' .. record .. '],"revision":9007199254740993}'

test("authorized records retain exact JSON numbers and unknown metadata", function()
    local status, body, headers, calls = run(original)
    assert(status == 200, "status " .. tostring(status))
    assert(body == expected, "JSON fidelity failed: " .. tostring(body))
    assert(#calls == 2 and calls[1].host == "ai-sse-keepalive-ingress-relay", "basic must run first")
    assert(calls[2].host == "apisix-models" and calls[2].port == 9080, "catalog leg must reach apisix-models directly")
    assert(calls[2].request.method == "GET" and calls[2].request.path == "/v1/models"
        and calls[2].request.query == "client_version=fixture", "catalog leg must be GET /v1/models with client_version passthrough")
    assert(calls[1].request.path == "/v1/models" and calls[2].request.path == "/v1/models",
        "models path changed")
    assert(calls[1].request.headers.Authorization == "Bearer fixture-only", "client auth changed")
    assert(headers["Cache-Control"] == "private, no-cache", "cache policy changed")
    assert(headers["Vary"] == "Authorization", "authorization variance lost")
end)

test("conditional response uses final exact entity", function()
    local _, _, headers = run(original)
    local status, body = run(original, {etag = headers.ETag})
    assert(status == 304 and body == "", "conditional response changed")
end)

test("basic auth error does not request enriched inventory", function()
    for _, rejection in ipairs({{401, '{"error":"denied"}'}, {404, ''}}) do
        local status, body, _, calls = run(original, {basic_status = rejection[1], basic = rejection[2]})
        assert(status == rejection[1] and body == rejection[2] and #calls == 1)
    end
end)

test("empty intersection and alternate list envelope", function()
    local status, body = run('{"object":"list","data":[{"id":"c/denied"}]}')
    assert(status == 200 and body == '{"object":"list","data":[]}')
end)

test("escaped collection keys and strings remain valid", function()
    local body = '{"mo\\u0064els":[' .. record .. ']}'
    local status, result = run(body)
    assert(status == 200 and result == body)
end)

test("ambiguous routing identities and collections fail closed", function()
    for _, body in ipairs({
        '{"models":[{"slug":"c/denied","slug":"c/allowed"}]}',
        '{"models":[{"slug":"c/allowed","id":"c/denied","i\\u0064":"c/allowed"}]}',
        '{"models":[{"slug":"c/denied"}],"mo\\u0064els":[{"slug":"c/allowed"}]}',
        '{"models":[{"slug":"c/allowed","id":"different"}]}',
        '{"models":{}}', '{"models":[null]}',
        '{"models":[{"slug":"c/allowed","value":NaN}]}',
    }) do
        local status = run(body)
        assert(status == 502, "ambiguous or invalid record accepted: " .. body)
    end
end)

test("admission queue and byte limits remain enforced", function()
    -- 阈值内直接放行；占满时短暂排队后仍拒绝，且不触达上游。
    local status = run(original, {active = 0})
    assert(status == 200, "in-threshold request must be admitted")
    local headers, calls
    status, _, headers, calls = run(original, {active = 1})
    assert(status == 503 and headers["Retry-After"] == "1" and #calls == 0)
    status = run(original, {headers = {["Content-Length"] = tostring(64 * 1024 * 1024 + 1)}})
    assert(status == 502, "original response size limit widened")
    status, _, _, calls = run(original, {basic_headers = {["Content-Length"] = tostring(64 * 1024 * 1024 + 1)}})
    assert(status == 502 and #calls == 1, "basic response limit changed or original requested")
end)

local function list(body, options)
    options = options or {}
    options.entry = "list"
    options.basic = body
    return run(nil, options)
end

test("plain list sorts bare IDs first on every plain route", function()
    local body = '{"data":[{"id":"z/a"},{"id":"b"},{"id":"A/b"},{"id":"B"},{"id":"a"},{"id":"a/b"}]}'
    local ordered = '{"data":[{"id":"B"},{"id":"a"},{"id":"b"},{"id":"A/b"},{"id":"a/b"},{"id":"z/a"}]}'
    for _, request in ipairs({
        {path = "/v1/models"},
        {path = "/v1/models", query = "client_version="},
        {path = "/v1/models", query = "foo=1"},
        {path = "/models", query = "foo=1"},
    }) do
        local status, result, _, calls, _, held = list(body, request)
        assert(status == 200 and result == ordered, "plain ordering failed: " .. tostring(result))
        assert(#calls == 1 and calls[1].host == "ai-sse-keepalive-ingress-relay" and calls[1].port == 8080)
        assert(calls[1].request.path == request.path and calls[1].request.query == request.query)
        assert(calls[1].request.headers.Authorization == "Bearer fixture-only")
        assert(calls[1].active == 1 and held, "plain list bypassed admission")
    end
end)

test("plain list uses decoded Unicode code-point order and stable equal IDs", function()
    local status, body = list([[{"data":[{"id":"\ud83d\ude00"},{"id":"\ue000"},{"id":"é"},{"id":"z"},{"id":"Z"}]}]])
    assert(status == 200 and body == [[{"data":[{"id":"Z"},{"id":"z"},{"id":"é"},{"id":"\ue000"},{"id":"\ud83d\ude00"}]}]])
    status, body = list('{"data":[{"id":"z"},{"id":"a","n":1},{"id":"a","n":2}]}')
    assert(status == 200 and body == '{"data":[{"id":"a","n":1},{"id":"a","n":2},{"id":"z"}]}')
end)

test("plain list preserves record fragments and envelope bytes", function()
    local prefix, suffix = ' {"revision":9007199254740993,"mo\\u0064els":[', '],"extra":{"n":9007199254740993}}\n'
    local bare = [[{"sl\u0075g":"A","id":"A","unknown":123456789012345678901234567890}]]
    local status, body = list(prefix .. record .. ', \n' .. bare .. suffix)
    assert(status == 200 and body == prefix .. bare .. ',' .. record .. suffix)
    local data_record = [[{"i\u0064":"z","vendor":{"n":9007199254740993,"text":"\\\" } ["}}]]
    prefix, suffix = '{"object":"list","da\\u0074a":[', '],"tail":null}'
    local first = '{"id":"A"}'
    status, body = list(prefix .. data_record .. ',' .. first .. suffix)
    assert(status == 200 and body == prefix .. first .. ',' .. data_record .. suffix)
    for _, empty in ipairs({'{"data":[],"object":"list"}', '{"models":[],"extra":false}'}) do
        status, body = list(empty)
        assert(status == 200 and body == empty)
    end
end)

test("plain list passes upstream errors through without reordering", function()
    for _, code in ipairs({401, 404, 429, 500}) do
        local raw = '{"data":[{"id":"z"},{"id":"a"}],"error":"unchanged"}'
        local status, body, headers, calls, warnings = list(raw, {
            basic_status = code,
            basic_headers = {ETag = '"upstream"', ["Cache-Control"] = "public", ["Retry-After"] = "5"},
        })
        assert(status == code and body == raw and #calls == 1 and #warnings == 0)
        assert(headers.ETag == nil and headers["Cache-Control"] == nil and headers["Retry-After"] == "5")
    end
end)

test("plain list passes unrecognized bodies through with WARN", function()
    for _, raw in ipairs({
        'not JSON', 'null', '[]', '{}', '{"data":{}}',
        '{"data":[],"models":[]}',
        '{"data":[],"da\\u0074a":[]}',
        '{"data":[null]}', '{"data":[[]]}', '{"data":["model"]}',
        '{"data":[{"id":""}]}', '{"data":[{"id":123}]}',
        '{"data":[{"id":"a","slug":"b"}]}',
        '{"data":[{"id":"a","i\\u0064":"a"}]}',
        '{"models":[{"slug":"a","sl\\u0075g":"a"}]}',
        '{"models":[{"slug":"a","id":"a","i\\u0064":"a"}]}',
        '{"models":[{"slug":"a","value":NaN}]}',
    }) do
        local status, body, headers, calls, warnings = list(raw, {
            basic_headers = {ETag = '"upstream"', ["Cache-Control"] = "public", ["X-Test"] = "retained"},
        })
        assert(status == 200 and body == raw and #calls == 1, "unrecognized body changed: " .. raw)
        assert(#warnings == 1, "missing WARN: " .. raw)
        assert(headers["X-Test"] == "retained" and headers.ETag == nil and headers["Cache-Control"] == nil)
    end
end)

test("plain list applies final-entity headers and conditionals", function()
    local raw = '{"data":[{"id":"z"},{"id":"a"}]}'
    local ordered = '{"data":[{"id":"a"},{"id":"z"}]}'
    local options = {basic_headers = {
        ETag = '"upstream"', ["Cache-Control"] = "public", ["Content-Length"] = tostring(#raw),
        ["Content-Type"] = "application/json; charset=utf-8", Vary = "Accept, authorization",
    }}
    local status, body, headers = list(raw, options)
    assert(status == 200 and body == ordered)
    assert(headers.ETag == '"apisix-' .. real_ngx.md5(ordered) .. '"')
    assert(headers["Cache-Control"] == "private, no-cache" and headers.Vary == "Accept, authorization")
    assert(headers["Content-Length"] == nil and headers["Content-Type"] == "application/json; charset=utf-8")
    status, body, headers = list(raw, {etag = headers.ETag})
    assert(status == 304 and body == "" and headers.Vary == "Authorization")
    assert(headers["Content-Type"] == "application/json" and headers["Cache-Control"] == "private, no-cache")
    status, body = list(raw, {etag = "W/" .. headers.ETag})
    assert(status == 304 and body == "")
    status, body = list(raw, {if_match = '"different"', etag = headers.ETag})
    assert(status == 412 and body == "", "If-Match must take precedence")
    status, body = list(raw, {if_match = "W/" .. headers.ETag})
    assert(status == 412 and body == "", "If-Match must use strong comparison")
    status, body = list(raw, {if_match = headers.ETag})
    assert(status == 200 and body == ordered)
    status, body = list(raw, {basic_status = 201})
    assert(status == 201 and body == ordered, "recognized success status changed")
end)

test("plain admission and response byte limits remain enforced", function()
    local status, _, headers, calls = list('{"data":[]}', {active = 1})
    assert(status == 503 and headers["Retry-After"] == "1" and #calls == 0)
    status, _, _, calls = list('{"data":[]}', {
        basic_headers = {["Content-Length"] = tostring(64 * 1024 * 1024 + 1)},
    })
    assert(status == 502 and #calls == 1)
end)

test("intersection keeps model-catalog order rather than sorting again", function()
    local raw = '{"models":[{"slug":"z/a"},{"slug":"c/allowed"},{"slug":"A"}]}'
    local status, body = run(raw, {basic = '{"data":[{"id":"A"},{"id":"c/allowed"},{"id":"z/a"}]}'})
    assert(status == 200 and body == raw)
end)

test("admit remains a slot-only entry point for the old route", function()
    local status, _, _, calls, _, held = run(nil, {entry = "admit"})
    assert(status == nil and #calls == 0 and held)
    status, _, _, calls, _, held = run(nil, {entry = "admit", active = 1})
    assert(status == 503 and #calls == 0 and not held)
end)

print("PASS all " .. total .. " front JSON contract checks")
