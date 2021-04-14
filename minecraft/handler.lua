local redis = require 'redis'
local http = require "socket.http"
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
    kong.log("new connection count: %d", count)
    if count == 1 then
        kong.log("scaling world up")
        local headers = { 
            Authorization="Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.MdtOSGktuwpjR8KcOkwbw0IkSPe1JuQadcZAhGie4m0"
        }
        local result, status, _, _ = http.request{
            url=string.format("http://mc-operator-minecraft-control-plane-inner.mc-operator-system.svc.cluster.local/v1/worlds/%s/1", name),
            headers=headers,
            method="POST",
        }
        assert(status == 200, "status not ok")
    end
end

local function remove_connection(name)
    local count = client:decr(name)
    kong.log(string.format("current connected: %d", count))
    if count == 0 then
        -- prevent thrashing, sleep for 5 mins, check connections and then kill the world
        kong.log(string.format("No connections sleeping and then shutting down if no connections remain", count))
        os.execute("sleep " .. tonumber(300))
        if client:get(name) == 0 then
            kong.info(string.format("None connected: shutting down: %s", name))
            local headers = { 
                Authorization="Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.MdtOSGktuwpjR8KcOkwbw0IkSPe1JuQadcZAhGie4m0"
            }
            local result, status, _, _ = http.request{
                url=string.format("http://mc-operator-minecraft-control-plane-inner.mc-operator-system.svc.cluster.local/v1/worlds/%s/0", name),
                headers=headers,
                method="POST",
            }
            assert(status == 200, "status not ok")
        end
        kong.log("Shutdown aborted")
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
    kong.log(string.format("Port: %d Maps to %s", port, name))
    add_connection(name)
end

function WorldScaler:log(conf)
    WorldScaler.super.log(self)
    kong.log("disconnect!")
    local port = kong.request.get_port()
    local name = get_mapping(port)
    kong.log(string.format("Port: %d Maps to %s", port, name))
    remove_connection(name)
end

return WorldScaler