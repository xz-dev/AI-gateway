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
    local active = options.active or 0
    local dict = {
        incr = function(_, _, delta) active = active + delta; return active end,
        expire = function() return true end,
        set = function(_, _, value) active = value end,
    }
    local fake = {
        var = {uri = "/v1/models", args = options.query or "client_version=fixture",
            remote_addr = "127.0.0.1", http_if_none_match = options.etag},
        req = {get_method = function() return "GET" end,
            get_headers = function() return {Authorization = "Bearer fixture-only"} end},
        header = {}, shared = {["model-list-intersection"] = dict}, md5 = real_ngx.md5,
    }
    package.loaded["apisix.core"] = {json = json, log = {error = function() end, warn = function() end}}
    package.loaded["resty.http"] = {new = function()
        return {
            set_timeouts = function() end,
            connect = function(self, host) self.host = host; return true end,
            request = function(self, request)
                calls[#calls + 1] = {host = self.host, request = request}
                local basic = self.host == "ai-sse-keepalive-ingress-relay"
                local body = basic and (options.basic or '{"models":[{"slug":"c/allowed"}]}') or original
                local headers = basic and {} or (options.headers or {})
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
    local ok, status, body = pcall(module.run, nil, ctx)
    module.release(nil, ctx)
    _G.ngx = real_ngx
    assert(ok, status)
    assert(active == (options.active or 0), "admission lease leaked")
    return status, body, fake.header, calls
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

test("existing admission and byte limits remain enforced", function()
    local status, _, headers, calls = run(original, {active = 2})
    assert(status == 503 and headers["Retry-After"] == "1" and #calls == 0)
    status = run(original, {headers = {["Content-Length"] = tostring(16 * 1024 * 1024 + 1)}})
    assert(status == 502, "original response size limit widened")
end)

print("PASS all " .. total .. " front JSON contract checks")
