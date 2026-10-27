#!/usr/bin/python
# Test-only shim of community.docker.docker_container_info (see module_utils/shim.py).
from ansible_collections.community.docker.plugins.module_utils.shim import compose, runtime, service_of, start


def main():
    module = start("docker_container_info")
    name = module.params["name"]
    rc, out, _ = compose(module, "ps", "-q", service_of(name))
    ident = out.strip()
    if rc or not ident:
        module.exit_json(changed=False, exists=False, container=None)
    rc, image, _ = runtime("inspect", "--format={{.Image}}", ident)
    container = {"Id": ident, "Name": "/" + name.lstrip("/"), "Image": image.strip(),
                 "State": {"Running": True, "Status": "running"}, "Config": {"Env": []}}
    module.exit_json(changed=False, exists=True, container=container)


if __name__ == "__main__":
    main()
