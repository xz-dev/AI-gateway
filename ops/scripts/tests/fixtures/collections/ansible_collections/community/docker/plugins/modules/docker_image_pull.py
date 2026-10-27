#!/usr/bin/python
# Test-only shim of community.docker.docker_image_pull (see module_utils/shim.py).
import json

from ansible_collections.community.docker.plugins.module_utils.shim import runtime, start


def main():
    module = start("docker_image_pull")
    name = module.params["name"]
    if module.check_mode:
        module.exit_json(changed=True, actions=["Pulled image " + name], image={})
    rc, out, err = runtime("pull", name)
    if rc:
        module.fail_json(msg="pull failed", rc=rc, stdout=out, stderr=err)
    rc, out, _ = runtime("image", "inspect", name)
    image = json.loads(out) if rc == 0 and out.startswith("{") else {}
    module.exit_json(changed=True, actions=["Pulled image " + name], image=image)


if __name__ == "__main__":
    main()
