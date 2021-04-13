local redis = require 'redis'

local params = {
    host = 'redis.redis.svc',
    port = 6379,
}

local WorldScaler = {
    PRIORITY = 7,
    VERSION = "2.0.1",
}

function WorldScaler:preread(conf)
    local port = kong.request.get_port() 
    local name = WorldScaler.get_mapping(port)
    WorldScaler.add_connection(name)
end

function WorldScaler:log(conf)
    local name = WorldScaler.get_mapping(conf.port)
    WorldScaler.remove_connection(name)
end

function WorldScaler:get_mapping(port)
    local client = redis.connect(params)
    local name = client:get(string.format("%d", port))
    if name == nil then
        -- Kubernetes thing
        print("Looking up in Kubernetes")
    end
    return name
end

function WorldScaler:add_connection(name)
    local client = redis.connect(params)
    local count = client:incr(name)
    print("new connection count: %d", count)
    if count == 1 then
        -- Kubernetes thing
        print("scaling world up")
    end
end

function WorldScaler:remove_connection(name)
    local client = redis.connect(params)
    local count = client:decr(name)
    print(string.format("current connected: %d", count))
    if count == 0 then
        print("no connection scaling down")
    end
end

return WorldScaler