load("@builtin//struct.star", "module")

def __filegroups(ctx, vars):
    return {}

__handlers = {}

def __step_config(ctx, vars, step_config):
    if vars.use_reclient:
        step_config["rules"].extend([
            {
                "name": "g.rust.rustc",
                "action": "g.rust.rustcRE",
                "timeout": "2m",
                "use_remote_exec_wrapper": True,
            },
        ])
        return step_config

    step_config["rules"].extend([
        {
            "name": "g.rust.rustc",
            "action": "g.rust.rustc",
            "remote": True,
            "timeout": "20m",
            # "debug": True,
        },
    ])
    return step_config

rust = module(
    "rust",
    step_config = __step_config,
    filegroups = __filegroups,
    handlers = __handlers,
)
