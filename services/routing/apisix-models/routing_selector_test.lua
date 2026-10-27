local home = os.getenv("APISIX_HOME") or "/usr/local/apisix"
package.path = home .. "/?.lua;" .. home .. "/deps/share/lua/5.1/?.lua;" ..
    home .. "/deps/share/lua/5.1/?/init.lua;" .. package.path
package.cpath = home .. "/deps/lib/lua/5.1/?.so;" ..
    home .. "/deps/lib64/lua/5.1/?.so;" .. package.cpath

local json = require("cjson.safe")
local source = (arg[0]:match("^(.*)/") or ".") .. "/routing_selector.lua"
local real_ngx = ngx
local total = 0

local function run(options)
    options = options or {}
    local calls = {index = 0, cache = 0}
    local headers = {Authorization = "Bearer caller", session_id = "session"}
    local body = options.body or json.encode({model = options.model or "unknown-bare-id", stream = true})
    local rewritten
    local response = '{"decision":"not_found","generation":7}'
    package.loaded["resty.http"] = {
        new = function()
            calls.index = calls.index + 1
            return {
                set_timeouts = function() end,
                connect = function() return true end,
                request = function()
                    local sent = false
                    return {
                        status = 200,
                        headers = {["Content-Length"] = tostring(#response)},
                        body_reader = function()
                            if sent then return nil end
                            sent = true
                            return response
                        end,
                    }
                end,
                set_keepalive = function() return true end,
                close = function() end,
            }
        end,
    }
    _G.ngx = {
        var = {uri = options.uri or "/v1/responses"},
        req = {
            read_body = function() end,
            get_body_data = function() return body end,
            get_body_file = function() return nil end,
            set_body_data = function(value) rewritten = value end,
            set_header = function(name, value) headers[name] = value end,
        },
        escape_uri = function(value) return value end,
        shared = {routing_lkg = {
            get = function() calls.cache = calls.cache + 1 return options.stale_decision end,
            set = function() calls.cache = calls.cache + 1 end,
            delete = function() calls.cache = calls.cache + 1 end,
        }},
    }
    local module = dofile(source)
    local ctx = {}
    local key = options.cpa_key
    if key == nil then key = "cpa-secret" end
    local ok, status, result, response_headers = pcall(module.run, nil, ctx, key)
    _G.ngx = real_ngx
    assert(ok, status)
    return status, result, response_headers, ctx, calls, headers, rewritten
end

local function test(name, fn)
    fn()
    total = total + 1
    print("PASS " .. name)
end

test("every bare ID reaches AISIX even when absent from the catalog", function()
    for _, model in ipairs({"unknown-bare-id", "claude-opus-5-5", "gpt-6.1-sol", "model + exact"}) do
        local status, _, _, ctx, calls, headers, rewritten = run({model = model})
        assert(status == nil and ctx.upstream_id == "aisix", model .. " must reach AISIX, got " .. tostring(status))
        assert(calls.index == 0 and calls.cache == 0)
        assert(headers.Authorization == "Bearer caller" and headers.session_id == "session")
        assert(rewritten == nil)
    end
end)

test("IDs containing a slash reach CPA without registration or catalog lookup", function()
    for _, model in ipairs({"xl/claude-opus-5-5", "unknown-provider/unknown-model", "provider/nested/model", "provider/*"}) do
        local status, _, _, ctx, calls, headers, rewritten = run({model = model})
        assert(status == nil and ctx.upstream_id == "cpa", model)
        assert(calls.index == 0 and calls.cache == 0)
        assert(headers.Authorization == "Bearer cpa-secret" and headers.session_id == "session")
        assert(rewritten == nil)
    end
end)

test("any slash selects CPA even when either side is empty", function()
    for _, model in ipairs({"/model", "provider/", "/", "//"}) do
        local status, _, _, ctx, calls, headers = run({model = model})
        assert(status == nil and ctx.upstream_id == "cpa", model)
        assert(calls.index == 0 and calls.cache == 0)
        assert(headers.Authorization == "Bearer cpa-secret")
    end
end)

test("stale catalog routing cannot override the literal slash rule", function()
    for _, case in ipairs({
        {model = "unknown-bare-id", stale_decision = "cpa", expected = "aisix"},
        {model = "unknown/model", stale_decision = "aisix", expected = "cpa"},
    }) do
        local status, _, _, ctx, calls = run(case)
        assert(status == nil and ctx.upstream_id == case.expected)
        assert(calls.index == 0 and calls.cache == 0)
    end
end)

test("missing CPA credentials fail closed without changing AISIX authentication", function()
    for _, key in ipairs({false, "", 42}) do
        local status, result, _, ctx, _, headers = run({model = "provider/model", cpa_key = key})
        assert(status == 503 and result:find('"code":"service_unavailable"', 1, true))
        assert(ctx.upstream_id == nil and headers.Authorization == "Bearer caller")
        local aisix_status, _, _, aisix_ctx = run({model = "bare-id", cpa_key = key})
        assert(aisix_status == nil and aisix_ctx.upstream_id == "aisix")
    end
end)

test("malformed or invalid models still fail before forwarding", function()
    for _, body in ipairs({"not-json", "[]", "{}", '{"model":""}', '{"model":42}',
        '{"model":"' .. string.rep("x", 1025) .. '"}'}) do
        local status, result, response_headers, ctx, calls, headers = run({body = body})
        assert(status == 400 and result:find('"code":"invalid_request_error"', 1, true))
        assert(response_headers["Content-Type"] == "application/json")
        assert(ctx.upstream_id == nil and calls.index == 0 and calls.cache == 0)
        assert(headers.Authorization == "Bearer caller")
        assert(not result:find("cpa-secret", 1, true) and not result:find("Bearer caller", 1, true))
    end
end)

test("existing prompt cache rewrite preserves model and nested arrays", function()
    for _, model in ipairs({"bare-id", "provider/model"}) do
        local status, _, _, _, _, _, rewritten = run({body = json.encode({
            model = model, prompt_cache_retention = "24h", input = {{role = "user", content = "test"}},
        })})
        local document = json.decode(rewritten)
        assert(status == nil and document and document.model == model)
        assert(document.prompt_cache_retention == nil and document.input[1].content == "test")
        assert(rewritten:find('"input":[', 1, true))
    end
end)

test("existing normal and expanded Responses body limits remain", function()
    local body = '{"model":"bare-id","padding":"' .. string.rep("x", 16 * 1024 * 1024) .. '"}'
    local status, _, _, ctx = run({body = body, uri = "/v1/chat/completions"})
    assert(status == 400 and ctx.upstream_id == nil)
    status, _, _, ctx = run({body = body, uri = "/v1/responses"})
    assert(status == nil and ctx.upstream_id == "aisix")
    local too_large = string.rep("x", 64 * 1024 * 1024 + 1)
    status, _, _, ctx = run({body = too_large, uri = "/v1/responses"})
    assert(status == 400 and ctx.upstream_id == nil)
end)

print("PASS all " .. total .. " routing selector checks")
