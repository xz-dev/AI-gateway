local json = require("cjson.safe").new()
json.decode_array_with_array_mt(true)
json.decode_invalid_numbers(false)

local ngx = ngx
local io_open = io.open
local type = type

local _M = {}

local MAX_REQUEST_BYTES = 16 * 1024 * 1024
local MAX_RESPONSES_BYTES = 64 * 1024 * 1024 -- bounded history expanded by the front keeper
local MAX_MODEL_BYTES = 1024

local JSON_HEADERS = { ["Content-Type"] = "application/json" }
local INVALID_BODY = '{"error":{"code":"invalid_request_error","message":"A valid top-level model is required.","type":"invalid_request_error"}}'
local UNAVAILABLE = '{"error":{"code":"service_unavailable","message":"Model routing is temporarily unavailable.","type":"server_error"}}'

local function request_body()
    local limit = ngx.var and ngx.var.uri == "/v1/responses" and MAX_RESPONSES_BYTES or MAX_REQUEST_BYTES
    ngx.req.read_body()
    local body = ngx.req.get_body_data()
    if body then
        if #body > limit then
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
    body = file:read(limit + 1)
    file:close()
    if not body or #body > limit then
        return nil
    end
    return body
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
    if not strip_prompt_cache_retention(document) then
        return 400, INVALID_BODY, JSON_HEADERS
    end

    if document.model:find("/", 1, true) then
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
