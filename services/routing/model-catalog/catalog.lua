-- Catalog membership script (docs/model-catalog.md, "Membership script").
-- The host calls select(m) once per candidate ID from CPA, AISIX and the static models.
-- Return true to keep it (optionally true, {source = ..., compile = ...}), or false, "reason".

-- CPA IDs without a CPA prefix that are still admitted, as ordered standard globs.
-- The last matching entry decides; a leading "!" excludes. Example: { "devin/*" }.
local EXTRA_CPA = {}

local function listed(id, rules)
  local keep = false
  for _, rule in ipairs(rules) do
    if rule:sub(1, 1) == "!" then
      if glob(rule:sub(2), id) then keep = false end
    elseif glob(rule, id) then
      keep = true
    end
  end
  return keep
end

function select(m)
  if m.sources.cpa then
    local extra = listed(m.id, EXTRA_CPA)
    local prefixed = m.has_prefix
    if m.mode ~= "prefix" then prefixed = m.id:find("/", 1, true) ~= nil end
    if prefixed or extra then
      -- Forced prefixes: listed IDs are compiled, other OAuth IDs pass through as CPA returned them.
      return true, { source = "cpa", compile = m.mode == "forced" and extra }
    end
  end
  if m.sources.aisix then return true, { source = "aisix" } end
  if m.sources.static then return true, { source = "static" } end
  return false, "CPA ID without a CPA prefix"
end
