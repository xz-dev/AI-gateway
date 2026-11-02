#!/usr/bin/python
# Test-only shim of community.docker.docker_host_info (see module_utils/shim.py).
from ansible_collections.community.docker.plugins.module_utils.shim import start


def main():
    module = start("docker_host_info")
    module.exit_json(changed=False, can_talk_to_docker=True,
                     host_info={"ServerVersion": "fixture", "OSType": "linux"})


if __name__ == "__main__":
    main()
