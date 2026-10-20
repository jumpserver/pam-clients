"""Create isolated local resources; never change the development Redis or DB."""

import argparse
import hashlib
import json
import os
import secrets
import socket
import subprocess
import time
from pathlib import Path

import psycopg2
import redis


def private_json(path, data):
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w") as stream:
        json.dump(data, stream, indent=2)
        stream.write("\n")


def docker(*args, input_text=None):
    result = subprocess.run(["docker", *args], input=input_text, text=True, capture_output=True)
    if result.returncode:
        # A failing SQL command could contain a password; keep its output private.
        raise RuntimeError("Docker operation failed; inspect the dedicated demo resources")
    return result.stdout.strip()


def pg_sql(config, command, database="postgres"):
    return docker("exec", "-i", config["pg_container"], "psql", "-X", "-v", "ON_ERROR_STOP=1",
                  "-U", config["pg_admin"], "-d", database, "-At", input_text=command)


def redis_acl(config):
    lines = ["user default off"]
    for user, password in config["redis_users"].items():
        digest = hashlib.sha256(password.encode()).hexdigest()
        rights = "~* +@all" if user == "pam_demo_admin" else (
            "~pam-demo:* +ping +get +set +incr +incrby +client|setname +client|setinfo +acl|whoami"
        )
        lines.append(f"user {user} on #{digest} {rights}")
    return "\n".join(lines) + "\n"


def prepare(root, pg_container, redis_port):
    inventory = root / "resources.json"
    if inventory.exists():
        if not (root / "prepared.json").exists():
            raise RuntimeError("A previous preparation is incomplete; inspect its private resources.json before retrying")
        print("Reusing existing demo resources; passwords and account identities are unchanged.")
        return json.loads(inventory.read_text())
    with socket.socket() as port_check:
        port_check.bind(("127.0.0.1", redis_port))
    pg_environment = json.loads(docker("inspect", pg_container, "--format", "{{json .Config.Env}}"))
    pg_admin = dict(pair.split("=", 1) for pair in pg_environment)["POSTGRES_USER"]
    tag = secrets.token_hex(3)
    prefix = f"pam_demo_{tag}"
    config = {
        "resource_version": 1, "tag": tag, "pg_container": pg_container, "pg_admin": pg_admin,
        "database": prefix, "pg_owner": prefix + "_owner",
        "pg_users": {prefix + "_a": secrets.token_urlsafe(24), prefix + "_b": secrets.token_urlsafe(24)},
        "redis_container": "pam-demo-redis-" + tag, "redis_volume": "pam-demo-redis-" + tag,
        "redis_port": redis_port,
        "redis_users": {"pam_demo_admin": secrets.token_urlsafe(24),
                        "pam_demo_a": secrets.token_urlsafe(24), "pam_demo_b": secrets.token_urlsafe(24)},
    }
    private_json(inventory, config)
    context = psycopg2.extensions.adapt
    # Identifiers are generated, values are quoted, and SQL travels via stdin.
    def quote_id(value):
        return '"' + value.replace('"', '""') + '"'

    def quote_value(value):
        return context(value).getquoted().decode()
    owner, db = quote_id(config["pg_owner"]), quote_id(config["database"])
    roles = [f"CREATE ROLE {owner} NOLOGIN;"]
    for username, password in config["pg_users"].items():
        roles.append(f"CREATE ROLE {quote_id(username)} LOGIN PASSWORD {quote_value(password)};")
    pg_sql(config, "\n".join(roles))
    pg_sql(config, f"CREATE DATABASE {db} OWNER {owner};")
    users = ", ".join(map(quote_id, config["pg_users"]))
    pg_sql(config, f"""
        REVOKE ALL ON DATABASE {db} FROM PUBLIC;
        GRANT CONNECT ON DATABASE {db} TO {users};
        REVOKE CREATE ON SCHEMA public FROM PUBLIC;
        CREATE TABLE public.pam_demo_activity (
            instance_id TEXT PRIMARY KEY, writes BIGINT NOT NULL DEFAULT 1,
            authenticated_user TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
        );
        ALTER TABLE public.pam_demo_activity OWNER TO {owner};
        GRANT USAGE ON SCHEMA public TO {users};
        GRANT SELECT, INSERT, UPDATE ON public.pam_demo_activity TO {users};
    """, config["database"])
    redis_dir = root / "redis"
    redis_dir.mkdir(mode=0o700)
    acl_path = redis_dir / "users.acl"
    acl_path.write_text(redis_acl(config))
    # Only password hashes are stored here; Redis's container user must read it.
    # The host parent directory remains private (0700).
    acl_path.chmod(0o644)
    docker("run", "-d", "--name", config["redis_container"], "--restart", "unless-stopped",
           "--label", "com.jumpserver.pam-demo=" + tag,
           "-p", f"127.0.0.1:{redis_port}:6379",
           "-v", f"{acl_path}:/etc/redis/users.acl:ro",
           "-v", f"{config['redis_volume']}:/data", "redis:7", "redis-server",
           "--aclfile", "/etc/redis/users.acl", "--appendonly", "yes")
    for _ in range(50):
        try:
            with redis.Redis(host="127.0.0.1", port=redis_port, username="pam_demo_a",
                             password=config["redis_users"]["pam_demo_a"], socket_timeout=1) as client:
                client.ping()
            break
        except redis.RedisError:
            time.sleep(0.1)
    else:
        raise RuntimeError("Dedicated Redis did not become ready")
    private_json(root / "prepared.json", {"ready": True})
    print(f"Created isolated PostgreSQL database {config['database']} and Redis {config['redis_container']}:{redis_port}.")
    return config


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True, type=Path)
    parser.add_argument("--pg-container", default="postgre-postgre-1")
    parser.add_argument("--redis-port", type=int, default=6388)
    args = parser.parse_args()
    prepare(args.root.resolve(), args.pg_container, args.redis_port)


if __name__ == "__main__":
    main()
