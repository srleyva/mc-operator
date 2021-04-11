import fileinput
import json
import fire


class Ports:
    _template = """{{ "spec": {{ "template": {{ "spec": {{"containers": [{{ "name": "proxy", "ports": {ports}, "env": [ {{ "name": "KONG_STREAM_LISTEN", "value": "{stream_listen}" }}]}}]}}}}}}}}"""

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
