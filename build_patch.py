import fileinput


template = """{{ "spec": {{ "template": {{ "spec": {{"containers": [{{ "name": "proxy", "env": [ {{ "name": "KONG_STREAM_LISTEN", "value": "{}" }}]}}]}}}}}}}}"""


if __name__ == "__main__":
	value = []
	for line in fileinput.input():
		port = line.replace('\n', '')
		value.append(f'0.0.0.0:{port}')

	print(template.format(', '.join(value)).replace('\n', '').replace('\t', ''))