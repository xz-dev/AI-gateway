#!/usr/bin/python
# Test-only shim of community.docker.docker_compose_v2 (see module_utils/shim.py).
from ansible_collections.community.docker.plugins.module_utils.shim import compose, start


def main():
    module = start("docker_compose_v2")
    p = module.params
    if module.check_mode:
        module.exit_json(changed=True, actions=[], containers=[])
    if p["state"] == "present":
        args = ["up", "-d"]
        if p["pull"] != "policy":
            args += ["--pull", p["pull"]]
        if p["recreate"] == "always":
            args.append("--force-recreate")
        elif p["recreate"] == "never":
            args.append("--no-recreate")
        if not p["dependencies"]:
            args.append("--no-deps")
        if p["build"] == "always":
            args.append("--build")
        elif p["build"] == "never":
            args.append("--no-build")
    elif p["state"] == "restarted":
        args = ["restart"] + ([] if p["dependencies"] else ["--no-deps"])
    else:
        module.fail_json(msg="shim supports state present and restarted only")
    rc, out, err = compose(module, *args, *(p["services"] or []))
    if rc:
        module.fail_json(msg="compose failed", rc=rc, stdout=out, stderr=err)
    module.exit_json(changed=True, actions=[], containers=[], stdout=out, stderr=err)


if __name__ == "__main__":
    main()
