local redis = require 'redis'
local http = require "socket.http"
local json = require 'JSON'
local bit32 = require 'bit32'

local params = {
    host = 'redis-master.default.svc.cluster.local',
    port = 6379,
}

local client = redis.connect(params)
local kong = kong

local Packet = {
    peek_count = 0,
    sock = {}
}

function Packet:new()
    local sock = assert(ngx.req.socket())
    local peek_count = 0
    local packet = Packet
    packet.peek_count = peek_count
    packet.sock = sock
    return packet
end

function Packet:read_varint()
    local num_read = 0
    local value = 0
    while (true) do
        local current = self:get_u8()
        local result = bit32.band(current, 127)
        value = bit32.bor(value, bit32.lshift(result, 7 * num_read))
        num_read = num_read + 1;

        if bit32.band(current, 128) == 0 then
            break
        end
    end
    return value
end

function Packet:get_u8()
    self.peek_count = self.peek_count + 1
    local bytes = assert(self.sock:peek(self.peek_count))
    local data = string.byte(bytes:sub(self.peek_count))
    kong.log.debug("Bytes for payload: ", data)
    return data
end

function Packet:read_bytes_as_string(count)
    local original_peek_count = self.peek_count
    self.peek_count = self.peek_count + count
    local bytes = assert(self.sock:peek(self.peek_count))
    bytes = bytes:sub(original_peek_count + 1)
    kong.log.debug("String for payload: ", bytes)
    return bytes
end

function Packet:is_ping()
    local length = self:read_varint()
    kong.log.debug("packet len is: ", length)

    local packet_id = self:read_varint()
    kong.log.debug("packet id is: ", packet_id)

    if packet_id == 0 then
        local mc_version = self:read_varint()
        kong.log.debug("minecraft version:", mc_version)
        local name_size = self:read_varint()
        kong.log.debug("hostname size:", mc_version)
        local name = self:read_bytes_as_string(name_size + 2) --Size plus unsigned short
        kong.log.debug("HostName: ", name)
        local status = self:read_varint()
        kong.log.debug("status: ", status)
        if status == 1 then
            kong.log.debug("Status Packet From Client")
            return true
        end
    end
    return false
end

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
        if status == 208 then -- World already scaled
            return
        end

        assert(status == 200, "status not ok")
        ngx.sleep(30)
    end
end

local function remove_connection(name)
    local count = client:decr(name)
    kong.log.debug(string.format("current connected: %d", count))
    if count <= 0 then
        client:set(name, 0)
        kong.log(string.format("None connected: sending shutdown signal to control plane: %s", name))
        local headers = { 
            Authorization="Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.MdtOSGktuwpjR8KcOkwbw0IkSPe1JuQadcZAhGie4m0"
        }
        local result, status, _, _ = http.request{
            url=string.format("http://mc-operator-minecraft-control-plane-inner.mc-operator-system.svc.cluster.local/v1/worlds/%s/0", name),
            headers=headers,
            method="PUT",
        }
        assert(status == 200 or status == 208, "status not ok")
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
    local packet = Packet.new()
    if packet:is_ping() then
        kong.log("Ping packet detected, not scaling")
        return
    end

    WorldScaler.super.preread(self)
    local port = kong.request.get_port()
    local name = get_mapping(port)
    add_connection(name)
end

function WorldScaler:log(conf)
    WorldScaler.super.log(self)
    local port = kong.request.get_port()
    local name = get_mapping(port)
    if tonumber(client:get(name)) <= 0 then
        kong.log("No connections on host")
        return
    end

    remove_connection(name)
end

return WorldScaler