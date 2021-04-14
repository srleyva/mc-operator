local redis = require 'redis'

local params = {
    host = 'redis-master.default.svc.cluster.local',
    port = 6379,
}

local client = redis.connect(params)
local kong = kong

local function get_mapping(port)
    local name = client:get(string.format("%d", port))
    if name == nil then
        -- Kubernetes thing
        kong.log("Looking up in Kubernetes: TODO")
        name = "orensx"
    end
    client:set(port, name)
    return name
end

local function add_connection(name)
    local count = client:incr(name)
    kong.log("new connection count: %d", count)
    if count == 1 then
        kong.log("scaling world up")
        kog.log("mc-operator-minecraft-control-plane-inner.mc-operator-system.svc.cluster.local")
    end
end

local function remove_connection(name)
    local count = client:decr(name)
    kong.log(string.format("current connected: %d", count))
    if count == 0 then
        kong.log("no connection scaling down")
    end
end

local BasePlugin = require "kong.plugins.base_plugin"

local WorldScaler = BasePlugin:extend()

WorldScaler.VERSION  = "1.0.0"
WorldScaler.PRIORITY = 1000000

function WorldScaler:new()
    WorldScaler.super.new(self, "minecraft-plugin")
  end

function WorldScaler:init_worker()
    WorldScaler.super.init_worker(self)
    kong.log("Connecting to redis")
    local ok = client:ping()
    if ok then
        kong.log("Connected to redist")
    end
end

function WorldScaler:preread(conf)
    WorldScaler.super.preread(self)
    local port = kong.request.get_port()
    kong.log(string.format("port: %d", port))
    local name = get_mapping(port)
    add_connection(name)
end

function WorldScaler:log(conf)
    WorldScaler.super.log(self)
    kong.log("disconnect!")
    local port = kong.request.get_port()
    local name = get_mapping(port)
    remove_connection(name)
end

return WorldScaler