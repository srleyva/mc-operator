local redis = require 'redis'
local http = require "socket.http"
local socket = require "socket"
local json = require 'JSON'

local params = {
    host = 'redis-master.default.svc.cluster.local',
    port = 6379,
}

local client = redis.connect(params)
local kong = kong

local function get_mapping(port)
    local name = client:get(string.format("%d", port))
    if name == nil then
        local headers = { 
            Authorization="Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.MdtOSGktuwpjR8KcOkwbw0IkSPe1JuQadcZAhGie4m0"
        }
        local result, status, _, _ = http.request{
            url="http://mc-operator-minecraft-control-plane-inner.mc-operator-system.svc.cluster.local/v1/worlds",
            headers=headers,
            method="GET",
        }
        assert(status == 200, "status not ok")
        local mapping = json.decode(result)
        name = mapping.name
        client:set(port, name)
    end
    return name
end

local function add_connection(name)
    local count = client:incr(name)
    if count == 1 then
        kong.log.debug(string.format("Scaling World up: %s", name))
        local headers = { 
            Authorization="Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.MdtOSGktuwpjR8KcOkwbw0IkSPe1JuQadcZAhGie4m0"
        }
        local result, status, _, _ = http.request{
            url=string.format("http://mc-operator-minecraft-control-plane-inner.mc-operator-system.svc.cluster.local/v1/worlds/%s/1", name),
            headers=headers,
            method="PUT",
        }
        assert(status == 200, "status not ok")
        socket.sleep(30)
    end
end

local function remove_connection(name)
    local count = client:decr(name)
    kong.log.debug(string.format("current connected: %d", count))
    if count <= 0 then
        kong.log(string.format("None connected: sending shutdown signal to control plane: %s", name))
        local headers = { 
            Authorization="Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.MdtOSGktuwpjR8KcOkwbw0IkSPe1JuQadcZAhGie4m0"
        }
        local result, status, _, _ = http.request{
            url=string.format("http://mc-operator-minecraft-control-plane-inner.mc-operator-system.svc.cluster.local/v1/worlds/%s/0", name),
            headers=headers,
            method="PUT",
        }
        assert(status == 200, "status not ok")
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
    local ok = client:ping()
    if not ok then
        kong.error("Can't connect to redist")
    end
end

function WorldScaler:preread(conf)
    WorldScaler.super.preread(self)
    local port = kong.request.get_port()
    local name = get_mapping(port)
    add_connection(name)
end

function WorldScaler:log(conf)
    WorldScaler.super.log(self)
    local port = kong.request.get_port()
    local name = get_mapping(port)
    remove_connection(name)
end

return WorldScaler