local http = require("resty.http")
local json = require("cjson.safe").new()
json.decode_array_with_array_mt(true)
json.decode_invalid_numbers(false)

local ngx = ngx
local io_open = io.open
local ipairs = ipairs
local pairs = pairs
local string_lower = string.lower
local table_concat = table.concat
local table_insert = table.insert
local tonumber = tonumber
local type = type

local _M = {}

local INDEX_HOST = "models-enricher"
local INDEX_PORT = 8090
local INDEX_PATH = "/routing-index"
local CONNECT_TIMEOUT_MS = 300
local SEND_TIMEOUT_MS = 300
local READ_TIMEOUT_MS = 1000
local KEEPALIVE_TIMEOUT_MS = 60000
local KEEPALIVE_POOL_SIZE = 100
local MAX_REQUEST_BYTES = 16 * 1024 * 1024
local MAX_INDEX_BYTES = 1024
local MAX_MODEL_BYTES = 1024

local JSON_HEADERS = { ["Content-Type"] = "application/json" }
local INVALID_BODY = '{"error":{"code":"invalid_request_error","message":"A valid top-level model is required.","type":"invalid_request_error"}}'
local MODEL_NOT_FOUND = '{"error":{"code":"model_not_found","message":"Model not found.","type":"invalid_request_error"}}'
local UNAVAILABLE = '{"error":{"code":"service_unavailable","message":"Model routing is temporarily unavailable.","type":"server_error"}}'

local function request_body()
    ngx.req.read_body()
    local body = ngx.req.get_body_data()
    if body then
        if #body > MAX_REQUEST_BYTES then
            return nil
        end
        return body
    end
    local path = ngx.req.get_body_file()
    if not path then
        return nil
    end
    local file = io_open(path, "rb")
    if not file then
        return nil
    end
    body = file:read(MAX_REQUEST_BYTES + 1)
    file:close()
    if not body or #body > MAX_REQUEST_BYTES then
        return nil
    end
    return body
end

local function response_header(headers, wanted)
    wanted = string_lower(wanted)
    for name, value in pairs(headers or {}) do
        if string_lower(name) == wanted then
            return value
        end
    end
end

local function read_index_body(httpc, response)
    local length = tonumber(response_header(response.headers, "content-length"))
    if length and length > MAX_INDEX_BYTES then
        httpc:close()
        return nil
    end
    if not response.body_reader then
        httpc:close()
        return nil
    end
    local chunks = {}
    local total = 0
    while true do
        local chunk, err = response.body_reader(MAX_INDEX_BYTES)
        if err then
            httpc:close()
            return nil
        end
        if not chunk then
            break
        end
        total = total + #chunk
        if total > MAX_INDEX_BYTES then
            httpc:close()
            return nil
        end
        table_insert(chunks, chunk)
    end
    httpc:set_keepalive(KEEPALIVE_TIMEOUT_MS, KEEPALIVE_POOL_SIZE)
    return table_concat(chunks)
end

local function lookup(model)
    local httpc = http.new()
    httpc:set_timeouts(CONNECT_TIMEOUT_MS, SEND_TIMEOUT_MS, READ_TIMEOUT_MS)
    local ok = httpc:connect(INDEX_HOST, INDEX_PORT)
    if not ok then
        return nil
    end
    local response = httpc:request({
        method = "GET",
        path = INDEX_PATH,
        query = "model=" .. ngx.escape_uri(model),
        headers = {Host = INDEX_HOST},
    })
    if not response then
        httpc:close()
        return nil
    end
    local body = read_index_body(httpc, response)
    if response.status ~= 200 or not body then
        return nil
    end
    local result = json.decode(body)
    if type(result) ~= "table" or type(result.decision) ~= "string" or
       type(result.generation) ~= "number" or result.generation < 0 or
       result.generation % 1 ~= 0 then
        return nil
    end
    if result.decision ~= "cpa" and result.decision ~= "aisix" and
       result.decision ~= "not_found" and result.decision ~= "unavailable" then
        return nil
    end
    if result.generation == 0 and result.decision ~= "unavailable" then
        return nil
    end
    return result.decision
end

local function strip_prompt_cache_retention(document)
    if document.prompt_cache_retention == nil then
        return true
    end
    document.prompt_cache_retention = nil
    local body = json.encode(document)
    if not body then
        return false
    end
    ngx.req.set_body_data(body)
    return true
end

function _M.run(_, ctx, cpa_api_key)
    local body = request_body()
    local document = body and json.decode(body)
    if type(document) ~= "table" or getmetatable(document) == json.array_mt or
       type(document.model) ~= "string" or document.model == "" or
       #document.model > MAX_MODEL_BYTES then
        return 400, INVALID_BODY, JSON_HEADERS
    end

    local decision = lookup(document.model)
    if decision == "not_found" then
        return 404, MODEL_NOT_FOUND, JSON_HEADERS
    end
    if decision == "unavailable" or not decision then
        return 503, UNAVAILABLE, JSON_HEADERS
    end
    if not strip_prompt_cache_retention(document) then
        return 400, INVALID_BODY, JSON_HEADERS
    end

    if decision == "cpa" then
        if type(cpa_api_key) ~= "string" or cpa_api_key == "" then
            return 503, UNAVAILABLE, JSON_HEADERS
        end
        ngx.req.set_header("Authorization", "Bearer " .. cpa_api_key)
        ctx.upstream_id = "cpa"
    else
        ctx.upstream_id = "aisix"
    end
end

return _M
