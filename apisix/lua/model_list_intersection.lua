local core = require("apisix.core")
local http = require("resty.http")

local ngx = ngx
local getmetatable = getmetatable
local ipairs = ipairs
local pairs = pairs
local setmetatable = setmetatable
local string_gmatch = string.gmatch
local string_lower = string.lower
local string_match = string.match
local table_concat = table.concat
local table_insert = table.insert
local tonumber = tonumber
local tostring = tostring
local type = type

local _M = {}

local UPSTREAM_HOST = "sub2api-apisix-relay"
local UPSTREAM_PORT = 8080
local BASIC_MAX_BYTES = 1024 * 1024
local ORIGINAL_MAX_BYTES = 16 * 1024 * 1024
local FINAL_MAX_BYTES = 16 * 1024 * 1024
local CONNECT_TIMEOUT_MS = 3000
local SEND_TIMEOUT_MS = 5000
local READ_TIMEOUT_MS = 60000
local KEEPALIVE_TIMEOUT_MS = 60000
local KEEPALIVE_POOL_SIZE = 100
local READ_CHUNK_BYTES = 64 * 1024
local ACTIVE_DICT_NAME = "model-list-intersection"
local ACTIVE_KEY = "active"
local ACTIVE_LEASE_SECONDS = 120
local MAX_INFLIGHT = 2

local ERROR_BODY =
    '{"error":{"code":"model_catalog_intersection_failed",' ..
    '"message":"Model catalog intersection failed.",' ..
    '"type":"gateway_error"}}'
local BUSY_BODY =
    '{"error":{"code":"model_catalog_busy",' ..
    '"message":"Model catalog gateway is busy.",' ..
    '"type":"gateway_error"}}'

local OMIT_REQUEST_HEADERS = {
    ["accept-encoding"] = true,
    ["cf-connecting-ip"] = true,
    ["cf-connecting-ipv6"] = true,
    ["connection"] = true,
    ["content-length"] = true,
    ["forwarded"] = true,
    ["host"] = true,
    ["if-match"] = true,
    ["if-modified-since"] = true,
    ["if-none-match"] = true,
    ["if-unmodified-since"] = true,
    ["keep-alive"] = true,
    ["proxy-authenticate"] = true,
    ["proxy-authorization"] = true,
    ["range"] = true,
    ["te"] = true,
    ["trailer"] = true,
    ["transfer-encoding"] = true,
    ["true-client-ip"] = true,
    ["upgrade"] = true,
    ["x-client-ip"] = true,
    ["x-forwarded-for"] = true,
    ["x-original-forwarded-for"] = true,
    ["x-real-ip"] = true,
}

local OMIT_RESPONSE_HEADERS = {
    ["cache-control"] = true,
    ["connection"] = true,
    ["content-encoding"] = true,
    ["content-length"] = true,
    ["date"] = true,
    ["etag"] = true,
    ["keep-alive"] = true,
    ["server"] = true,
    ["transfer-encoding"] = true,
}

local function response_header(headers, wanted)
    wanted = string_lower(wanted)
    for name, value in pairs(headers or {}) do
        if string_lower(name) == wanted then
            return value
        end
    end
end

local function copy_request_headers()
    local copied = {}
    for name, value in pairs(ngx.req.get_headers()) do
        if not OMIT_REQUEST_HEADERS[string_lower(name)] then
            if type(value) == "table" then
                value = table_concat(value, ", ")
            end
            copied[name] = value
        end
    end
    local client_ip = ngx.var.remote_addr
    copied.Host = UPSTREAM_HOST
    copied["CF-Connecting-IP"] = client_ip
    copied["X-Forwarded-For"] = client_ip
    copied["X-Real-IP"] = client_ip
    return copied
end

local function read_body(httpc, response, max_bytes)
    if not response.body_reader then
        httpc:close()
        return nil, "upstream response has no body"
    end

    local content_length_header = response_header(response.headers, "content-length")
    local content_length = content_length_header and tonumber(content_length_header)
    if content_length and content_length > max_bytes then
        httpc:close()
        return nil, "response body exceeds limit"
    end

    local chunks = {}
    local total = 0
    while true do
        local chunk, err = response.body_reader(READ_CHUNK_BYTES)
        if err then
            httpc:close()
            return nil, "failed reading response body: " .. tostring(err)
        end
        if not chunk then
            break
        end

        total = total + #chunk
        if total > max_bytes then
            httpc:close()
            return nil, "response body exceeds limit"
        end
        table_insert(chunks, chunk)
    end

    httpc:set_keepalive(KEEPALIVE_TIMEOUT_MS, KEEPALIVE_POOL_SIZE)
    return table_concat(chunks)
end

local function fetch(path, query, max_bytes, headers)
    local httpc = http.new()
    httpc:set_timeouts(CONNECT_TIMEOUT_MS, SEND_TIMEOUT_MS, READ_TIMEOUT_MS)

    local ok, err = httpc:connect(UPSTREAM_HOST, UPSTREAM_PORT)
    if not ok then
        return nil, "failed connecting to upstream: " .. tostring(err)
    end

    local response, request_err = httpc:request({
        method = "GET",
        path = path,
        query = query,
        headers = headers,
    })
    if not response then
        httpc:close()
        return nil, "failed requesting upstream: " .. tostring(request_err)
    end

    local body, body_err = read_body(httpc, response, max_bytes)
    if not body then
        return nil, body_err
    end

    return {
        status = response.status,
        headers = response.headers,
        body = body,
    }
end

local function catalog(document)
    if type(document) ~= "table" then
        return nil, "catalog is not an object"
    end

    local has_data = document.data ~= nil
    local has_models = document.models ~= nil
    if has_data == has_models then
        return nil, "catalog must contain exactly one model collection"
    end

    local key = has_data and "data" or "models"
    local items = document[key]
    if type(items) ~= "table" or getmetatable(items) ~= core.json.array_mt then
        return nil, "catalog model collection is not an array"
    end
    return {
        key = key,
        id_field = has_data and "id" or "slug",
        items = items,
    }
end

local function model_id(item, id_field)
    if type(item) ~= "table" then
        return nil, "model entry is not an object"
    end

    local id = item[id_field]
    if type(id) ~= "string" or id == "" then
        return nil, "model entry has no valid " .. id_field
    end

    local alternate_field = id_field == "id" and "slug" or "id"
    local alternate = item[alternate_field]
    if alternate ~= nil and
       (type(alternate) ~= "string" or alternate == "" or alternate ~= id) then
        return nil, "model entry has conflicting " .. alternate_field
    end
    return id
end

local function allowed_ids(document)
    local source, err = catalog(document)
    if not source then
        return nil, err
    end

    local allowed = {}
    for _, item in ipairs(source.items) do
        local id, id_err = model_id(item, source.id_field)
        if not id then
            return nil, id_err
        end
        allowed[id] = true
    end
    return allowed
end

local function filter_original(document, allowed)
    local source, err = catalog(document)
    if not source then
        return nil, err
    end

    local filtered = setmetatable({}, core.json.array_mt)
    for _, item in ipairs(source.items) do
        local id, id_err = model_id(item, source.id_field)
        if not id then
            return nil, id_err
        end
        if allowed[id] then
            table_insert(filtered, item)
        end
    end

    document[source.key] = filtered
    return document
end

local function apply_upstream_headers(headers)
    for name, value in pairs(headers or {}) do
        if not OMIT_RESPONSE_HEADERS[string_lower(name)] then
            ngx.header[name] = value
        end
    end
end

local function gateway_error(reason)
    core.log.error("model-list intersection failed: ", tostring(reason or "unknown error"))
    ngx.header["Content-Type"] = "application/json"
    ngx.header["Cache-Control"] = "no-store"
    return 502, ERROR_BODY
end

local function busy_response(reason)
    core.log.warn("model-list intersection rejected: ", tostring(reason or "busy"))
    ngx.header["Content-Type"] = "application/json"
    ngx.header["Cache-Control"] = "no-store"
    ngx.header["Retry-After"] = "1"
    return 503, BUSY_BODY
end

local function acquire(ctx)
    local dict = ngx.shared[ACTIVE_DICT_NAME]
    if not dict then
        return nil, "shared admission dictionary is unavailable"
    end

    local active, err = dict:incr(ACTIVE_KEY, 1, 0)
    if not active then
        return nil, "failed acquiring admission slot: " .. tostring(err)
    end
    if active > MAX_INFLIGHT then
        dict:incr(ACTIVE_KEY, -1)
        return nil, "concurrency limit reached"
    end

    dict:expire(ACTIVE_KEY, ACTIVE_LEASE_SECONDS)
    ctx.model_list_intersection_acquired = true
    return true
end

function _M.release(_, ctx)
    if not ctx or not ctx.model_list_intersection_acquired then
        return
    end
    ctx.model_list_intersection_acquired = nil

    local dict = ngx.shared[ACTIVE_DICT_NAME]
    if not dict then
        return
    end

    local active = dict:incr(ACTIVE_KEY, -1)
    if active and active < 0 then
        dict:set(ACTIVE_KEY, 0, ACTIVE_LEASE_SECONDS)
    end
end

local function etag_matches(header, etag, weak)
    if not header then
        return false
    end

    local expected = weak and string_match(etag, "^[Ww]/(.*)$") or etag
    expected = expected or etag
    for value in string_gmatch(header, "[^,]+") do
        value = string_match(value, "^%s*(.-)%s*$")
        if value == "*" then
            return true
        end
        if weak then
            value = string_match(value, "^[Ww]/(.*)$") or value
        end
        if value == expected then
            return true
        end
    end
    return false
end

local function merge_vary(value)
    local values = type(value) == "table" and value or {value}
    local merged = {}
    local seen = {}

    for _, entry in ipairs(values) do
        if entry then
            for token in string_gmatch(entry, "[^,]+") do
                token = string_match(token, "^%s*(.-)%s*$")
                if token == "*" then
                    return "*"
                end
                local normalized = string_lower(token)
                if token ~= "" and not seen[normalized] then
                    seen[normalized] = true
                    table_insert(merged, token)
                end
            end
        end
    end

    if not seen.authorization then
        table_insert(merged, "Authorization")
    end
    return table_concat(merged, ", ")
end

function _M.run(_, ctx)
    local path = ngx.var.uri
    if ngx.req.get_method() ~= "GET" or
       (path ~= "/v1/models" and path ~= "/models") then
        return
    end

    local raw_query = ngx.var.args
    if not raw_query or raw_query == "" then
        return
    end

    local acquired, acquire_err = acquire(ctx)
    if not acquired then
        return busy_response(acquire_err)
    end

    local headers = copy_request_headers()
    local original_thread = ngx.thread.spawn(
        fetch, path, raw_query, ORIGINAL_MAX_BYTES, headers)
    local basic_thread = ngx.thread.spawn(
        fetch, path, nil, BASIC_MAX_BYTES, headers)

    local basic_ok, basic, basic_err = ngx.thread.wait(basic_thread)
    if not basic_ok or not basic then
        ngx.thread.kill(original_thread)
        return gateway_error(basic_err or basic)
    end
    if basic.status < 200 or basic.status >= 300 then
        ngx.thread.kill(original_thread)
        apply_upstream_headers(basic.headers)
        return basic.status, basic.body
    end

    local original_ok, original, original_err = ngx.thread.wait(original_thread)
    if not original_ok or not original then
        return gateway_error(original_err or original)
    end
    if original.status < 200 or original.status >= 300 then
        apply_upstream_headers(original.headers)
        return original.status, original.body
    end

    local basic_document, basic_decode_err = core.json.decode(basic.body)
    if not basic_document then
        return gateway_error("invalid basic catalog: " .. tostring(basic_decode_err))
    end

    local original_document, original_decode_err = core.json.decode(original.body)
    if not original_document then
        return gateway_error("invalid original catalog: " .. tostring(original_decode_err))
    end

    local allowed, allowed_err = allowed_ids(basic_document)
    if not allowed then
        return gateway_error("invalid basic catalog: " .. tostring(allowed_err))
    end

    local filtered, filter_err = filter_original(original_document, allowed)
    if not filtered then
        return gateway_error("invalid original catalog: " .. tostring(filter_err))
    end

    local body, encode_err = core.json.encode(filtered)
    if not body then
        return gateway_error("failed encoding filtered catalog: " .. tostring(encode_err))
    end
    if #body > FINAL_MAX_BYTES then
        return gateway_error("filtered catalog exceeds limit")
    end

    local etag = '"apisix-' .. ngx.md5(body) .. '"'
    apply_upstream_headers(original.headers)
    ngx.header["Content-Type"] =
        response_header(original.headers, "content-type") or "application/json"
    ngx.header["Cache-Control"] = "private, no-cache"
    ngx.header["ETag"] = etag
    ngx.header["Vary"] = merge_vary(response_header(original.headers, "vary"))

    if ngx.var.http_if_match and
       not etag_matches(ngx.var.http_if_match, etag, false) then
        return 412, ""
    end
    if etag_matches(ngx.var.http_if_none_match, etag, true) then
        return 304, ""
    end

    return 200, body
end

return _M
