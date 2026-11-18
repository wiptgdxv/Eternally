local noop = 2 -- kNoop: Rime and the target app continue handling the key.

local function publish(kind, text)
  local pipe = io.open("\\\\.\\pipe\\LoTranslationPreview-v1", "r+b")
  if not pipe then return end
  pipe:write(kind, text or "")
  pipe:close()
end

local function init(env)
  env.commit = env.engine.context.commit_notifier:connect(function(ctx)
    if ctx:get_option("ascii_mode") then return end
    local text = ctx:get_commit_text()
    if text and #text > 0 and #text <= 4096 then
      publish("T", text)
    end
  end)
end

local function fini(env)
  if env.commit then env.commit:disconnect() end
end

local function func(key, env)
  if key:release() then return noop end
  local ctx = env.engine.context
  if ctx:is_composing() or ctx:get_option("ascii_mode") then return noop end
  local name = key:repr()
  if name == "BackSpace" then
    publish("B")
  elseif name == "Return" or name == "KP_Enter" or name == "Escape" or name == "Tab"
    or name == "Left" or name == "Right" or name == "Up" or name == "Down"
    or name == "Home" or name == "End" or name == "Delete"
    or name == "Control+BackSpace" or name == "Control+Delete"
    or name == "Control+x" or name == "Control+z" or name == "Control+v" then
    publish("R")
  end
  return noop
end

return { init = init, fini = fini, func = func }
