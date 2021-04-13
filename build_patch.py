import fileinput
import json
import fire


class Ports:
    _template = """{{ "spec": {{ "template": {{ "spec": {{"containers": [{{ "name": "proxy", "ports": {ports}, "env": [ {{ "name": "KONG_STREAM_LISTEN", "value": "{stream_listen}" }}]}}]}}}}}}}}"""

    _lua_plugins = """{{"spec": {{ "template": {{ "spec": {{ "containers": [{{ "name": "proxy", "env": [ {{ "name": "KONG_PLUGINS", "value": "{plugins}" }}, {{"name": "KONG_LUA_PACKAGE_PATH", "value": "/opt/?.lua;;"}}], "volumeMounts": [ {volume_mounts} ]}}], "volumes": [ {volumes} ]}}}}}}}}"""

    def generate_lua_patch(self, plugins=["bundled", "scale", "redis", "http"], mounts=["scale", "redis", "http", "http-compat"]):
        volume_mount_template = '{{ "name": "kong-plugin-{name}", "mountPath": "/opt/kong/plugins/{path}" }}'
        volume_template = '{{ "name": "kong-plugin-{name}", "configMap": {{ "name": "kong-plugin-{name}" }} }}'

        volume_mounts = []
        volumes = []
        for mount in mounts:
            volume_mounts.append(volume_mount_template.format(
                name=mount, path=mount.replace("-", "/")))
            volumes.append(volume_template.format(name=mount))
        print(self._lua_plugins.format(
            plugins=",".join(plugins),
            volume_mounts=",".join(volume_mounts),
            volumes=",".join(volumes),
        ))

    def generate_ports(self, min_port=1025, max_port=2025):
        if min_port < 1024:
            raise RuntimeError(f"privledged ports in range: {min_port}")
        if max_port > 65535:
            raise RuntimeError(f"port max out of range: {max}")

        ports = []
        for i in range(min_port, max_port):
            ports.append({
                "containerPort": i,
                "name": f"stream{i}",
                "protocol": "TCP"
            })

        stream_listen = [
            f'0.0.0.0:{port["containerPort"]}' for port in ports]

        print(self._template.format(
            ports=json.dumps(ports),
            stream_listen=', '.join(stream_listen)))


if __name__ == "__main__":
    fire.Fire(Ports)
