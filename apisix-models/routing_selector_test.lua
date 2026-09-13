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
    local calls = {}
    local headers = {Authorization = options.authorization or "Bearer caller", session_id = "session"}
    local body = options.body or '{"model":"axis/model + exact","stream":true}'
    local rewritten
    local response_body = options.lookup_body or ('{"decision":"' .. (options.decision or "cpa") .. '","generation":7}')
    local fake = {
        req = {
            read_body = function() end,
            get_body_data = function() return body end,
            get_body_file = function() return nil end,
            set_body_data = function(value) rewritten = value end,
            set_header = function(name, value) headers[name] = value end,
        },
        escape_uri = function(value)
            return (value:gsub("%%", "%%25"):gsub("/", "%%2F"):gsub(" ", "%%20"):gsub("%+", "%%2B"))
        end,
    }
    package.loaded["resty.http"] = {
        new = function()
            return {
                set_timeouts = function() end,
                connect = function()
                    if options.connect_error then return nil, "connect failed" end
                    return true
                end,
                request = function(_, request)
                    calls[#calls + 1] = request
                    if options.request_error then return nil, "request failed" end
                    local sent = false
                    return {
                        status = options.lookup_status or 200,
                        headers = { ["Content-Length"] = tostring(#response_body) },
                        body_reader = function()
                            if sent then return nil end
                            sent = true
                            return response_body
                        end,
                    }
                end,
                set_keepalive = function() return true end,
                close = function() end,
            }
        end,
    }
    _G.ngx = fake
    local module = dofile(source)
    local ctx = {}
    local ok, status, result, response_headers = pcall(module.run, nil, ctx, "cpa-secret")
    _G.ngx = real_ngx
    assert(ok, status)
    return status, result, response_headers, ctx, calls, headers, rewritten
end

local function test(name, fn)
    fn()
    total = total + 1
    print("PASS " .. name)
end

test("CPA decision selects one upstream and injects only the CPA credential", function()
    local status, _, _, ctx, calls, headers = run({decision = "cpa"})
    assert(status == nil and ctx.upstream_id == "cpa")
    assert(#calls == 1 and calls[1].method == "GET")
    assert(calls[1].path == "/routing-index")
    assert(calls[1].query == "model=axis%2Fmodel%20%2B%20exact")
    assert(headers.Authorization == "Bearer cpa-secret")
    assert(headers.session_id == "session")
end)

test("AISIX decision preserves the caller credential and exact body", function()
    local status, _, _, ctx, calls, headers, rewritten = run({decision = "aisix"})
    assert(status == nil and ctx.upstream_id == "aisix" and #calls == 1)
    assert(headers.Authorization == "Bearer caller")
    assert(rewritten == nil)
end)

test("not found and unavailable fail without an upstream", function()
    local status, body, response_headers, ctx, calls = run({decision = "not_found"})
    assert(status == 404 and body:find('"code":"model_not_found"', 1, true))
    assert(not body:find("cpa-secret", 1, true) and not body:find("Bearer caller", 1, true) and not body:find("axis/model", 1, true))
    assert(response_headers["Content-Type"] == "application/json" and ctx.upstream_id == nil and #calls == 1)
    local unavailable_status, unavailable_body, _, unavailable_ctx, unavailable_calls = run({decision = "unavailable"})
    assert(unavailable_status == 503 and unavailable_body:find('"code":"service_unavailable"', 1, true))
    assert(unavailable_ctx.upstream_id == nil and #unavailable_calls == 1)
end)

test("lookup failures never become not found or a backend selection", function()
    for _, options in ipairs({
        {connect_error = true},
        {request_error = true},
        {lookup_status = 500},
        {lookup_body = '{"decision":"unknown","generation":7}'},
        {lookup_body = '{"decision":"cpa"}'},
        {lookup_body = string.rep("x", 1025)},
    }) do
        local status, body, _, ctx = run(options)
        assert(status == 503 and body:find('"code":"service_unavailable"', 1, true))
        assert(ctx.upstream_id == nil)
    end
end)

test("malformed classified payloads do not perform a lookup", function()
    for _, body in ipairs({"not-json", "[]", "{}", '{"model":""}', '{"model":42}'}) do
        local status, result, _, ctx, calls = run({body = body})
        assert(status == 400 and result:find('"code":"invalid_request_error"', 1, true))
        assert(not result:find("cpa-secret", 1, true) and not result:find("Bearer caller", 1, true))
        assert(ctx.upstream_id == nil and #calls == 0)
    end
end)

test("existing prompt cache compatibility rewrite remains scoped", function()
    local _, _, _, ctx, _, _, rewritten = run({
        decision = "cpa",
        body = '{"model":"cpa/model","prompt_cache_retention":"24h","value":1}',
    })
    assert(ctx.upstream_id == "cpa" and rewritten)
    assert(not rewritten:find("prompt_cache_retention", 1, true))
    local document = json.decode(rewritten)
    assert(document and document.model == "cpa/model", rewritten)
end)

print("PASS all " .. total .. " routing selector checks")
