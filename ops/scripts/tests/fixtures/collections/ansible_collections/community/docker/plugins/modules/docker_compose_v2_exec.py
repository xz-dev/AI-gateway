#!/usr/bin/python
# Test-only shim of community.docker.docker_compose_v2_exec (see module_utils/shim.py).
import shlex

from ansible_collections.community.docker.plugins.module_utils.shim import compose, start


def main():
    module = start("docker_compose_v2_exec")
    p = module.params
    if module.check_mode:
        module.fail_json(msg="docker_compose_v2_exec does not support check mode; set check_mode: false")
    argv = shlex.split(p["command"]) if p["command"] is not None else p["argv"]
    stdin = p["stdin"]
    if stdin is not None and p["stdin_add_newline"]:
        stdin += "\n"
    flags = [] if p["tty"] else ["-T"]
    rc, out, err = compose(module, "exec", *flags, p["service"], *argv, stdin=stdin)
    module.exit_json(changed=True, rc=rc, stdout=out, stderr=err)


if __name__ == "__main__":
    main()
