"""Generate local-only credentials; never print credentials or commit the output."""
import hashlib
import json
import pathlib
import secrets

root = pathlib.Path(".dev/console")
root.mkdir(parents=True, exist_ok=True)


def secret(name):
    path = root / name
    if not path.exists():
        path.write_text(secrets.token_urlsafe(32))
    path.chmod(0o600)
    return path.read_text().strip()


password = secret("password")
agents = []
for cluster, name in [("local-qa", "Local QA"), ("local-prod", "Local Production")]:
    token = secret(cluster + "-token")
    agents.append({"id": cluster, "name": name, "tokenHash": hashlib.sha256(token.encode()).hexdigest()})
viewer = secret("viewer-token")
config = {
    "publicURL": "http://127.0.0.1:8876",
    "agents": agents,
    "readers": [{"subject": "local-viewer", "tokenHash": hashlib.sha256(viewer.encode()).hexdigest(),
                 "clusters": [a["id"] for a in agents]}],
}
for name, data in {
    "server-config.json": json.dumps(config, indent=2),
    "database-url": f"postgres://postgres:{password}@127.0.0.1:15432/optipod?sslmode=disable",
    "database-url-in-cluster": f"postgres://postgres:{password}@postgres:5432/optipod?sslmode=disable",
}.items():
    path = root / name
    path.write_text(data)
    path.chmod(0o600)
