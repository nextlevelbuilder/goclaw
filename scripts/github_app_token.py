#!/usr/bin/env python3
from __future__ import annotations

import argparse
import base64
import json
import os
import re
import subprocess
import sys
import time
from datetime import UTC, datetime
from pathlib import Path
from typing import Any
from urllib import error, parse, request


def _b64url(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode("ascii")


def normalize_repo_url(url: str) -> str:
    out = url.strip()
    if out.startswith("git@github.com:"):
        return "https://github.com/" + out.removeprefix("git@github.com:")
    if out.startswith("ssh://git@github.com/"):
        return "https://github.com/" + out.removeprefix("ssh://git@github.com/")
    return out.rstrip("/")


def extract_repo_slug(value: str) -> tuple[str, str] | None:
    normalized = normalize_repo_url(value)
    match = re.match(r"^(?:https://github\.com/)?([^/]+)/([^/]+?)(?:\.git)?$", normalized, re.IGNORECASE)
    if not match:
        return None
    owner = match.group(1).strip()
    repo = match.group(2).strip()
    if not owner or not repo:
        return None
    return owner, repo


class GitHubAppHelper:
    def __init__(self) -> None:
        self.app_id = os.getenv("GOCLAW_GITHUB_APP_ID", "").strip()
        self.api_base = os.getenv("GOCLAW_GITHUB_APP_API_BASE_URL", "https://api.github.com").strip().rstrip("/")
        self.default_owner = os.getenv("GOCLAW_GITHUB_APP_DEFAULT_OWNER", "").strip()
        self.allowed_owners = {
            item.strip().lower()
            for item in os.getenv("GOCLAW_GITHUB_APP_ALLOWED_OWNERS", "").split(",")
            if item.strip()
        }
        default_cache_dir = os.getenv("GOCLAW_GITHUB_APP_CACHE_DIR", "").strip()
        if not default_cache_dir:
            default_cache_dir = self._default_cache_dir()
        self.cache_dir = Path(default_cache_dir).expanduser().resolve()
        self.cache_dir.mkdir(parents=True, exist_ok=True)
        self.cache_path = self.cache_dir / "token-cache.json"
        self.private_key_path = self._resolve_private_key_path()
        if not self.app_id or not self.private_key_path.exists():
            raise SystemExit("GitHub App helper is not configured")

    def _default_cache_dir(self) -> str:
        container_dir = Path("/app/data/.runtime/github-app")
        if container_dir.parent.exists():
            return str(container_dir)
        return str(Path.home() / ".cache" / "goclaw" / "github-app")

    def _resolve_private_key_path(self) -> Path:
        explicit = os.getenv("GOCLAW_GITHUB_APP_PRIVATE_KEY_FILE", "").strip()
        if explicit:
            path = Path(explicit).expanduser().resolve()
            if not path.exists():
                raise SystemExit(f"GitHub App private key file not found: {path}")
            return path
        b64_value = os.getenv("GOCLAW_GITHUB_APP_PRIVATE_KEY_B64", "").strip()
        if not b64_value:
            raise SystemExit("missing GOCLAW_GITHUB_APP_PRIVATE_KEY_FILE or GOCLAW_GITHUB_APP_PRIVATE_KEY_B64")
        path = self.cache_dir / "github-app.private-key.pem"
        if not path.exists():
            path.write_bytes(base64.b64decode(b64_value))
            path.chmod(0o600)
        return path

    def _load_cache(self) -> dict[str, Any]:
        if not self.cache_path.exists():
            return {}
        try:
            return json.loads(self.cache_path.read_text(encoding="utf-8"))
        except Exception:
            return {}

    def _write_cache(self, payload: dict[str, Any]) -> None:
        tmp = self.cache_path.with_suffix(".tmp")
        tmp.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
        tmp.replace(self.cache_path)

    def _build_jwt(self) -> str:
        now = int(time.time())
        header = {"alg": "RS256", "typ": "JWT"}
        payload = {"iat": now - 60, "exp": now + 540, "iss": self.app_id}
        signing_input = (
            _b64url(json.dumps(header, separators=(",", ":"), sort_keys=True).encode("utf-8"))
            + "."
            + _b64url(json.dumps(payload, separators=(",", ":"), sort_keys=True).encode("utf-8"))
        )
        proc = subprocess.run(
            ["openssl", "dgst", "-binary", "-sha256", "-sign", str(self.private_key_path)],
            input=signing_input.encode("utf-8"),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        if proc.returncode != 0:
            raise SystemExit(proc.stderr.decode("utf-8", errors="replace").strip() or "failed to sign GitHub App JWT")
        return signing_input + "." + _b64url(proc.stdout)

    def _request_json(self, method: str, path: str, *, bearer: str, payload: dict[str, Any] | None = None) -> dict[str, Any]:
        body = None
        if payload is not None:
            body = json.dumps(payload).encode("utf-8")
        req = request.Request(
            self.api_base + path,
            data=body,
            method=method,
            headers={
                "Accept": "application/vnd.github+json",
                "Authorization": f"Bearer {bearer}",
                "User-Agent": "goclaw-github-app-helper",
                "X-GitHub-Api-Version": "2022-11-28",
            },
        )
        if body is not None:
            req.add_header("Content-Type", "application/json")
        try:
            with request.urlopen(req, timeout=30) as resp:
                raw = resp.read()
        except error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="replace").strip()
            raise SystemExit(f"GitHub API {exc.code} {path}: {detail}")
        except error.URLError as exc:
            raise SystemExit(f"GitHub API request failed for {path}: {exc}")
        parsed = json.loads(raw.decode("utf-8"))
        if not isinstance(parsed, dict):
            raise SystemExit(f"Unexpected GitHub API response for {path}")
        return parsed

    def _parse_expiry(self, value: str) -> float:
        raw = value.strip()
        if raw.endswith("Z"):
            raw = raw[:-1] + "+00:00"
        parsed = datetime.fromisoformat(raw)
        if parsed.tzinfo is None:
            parsed = parsed.replace(tzinfo=UTC)
        return parsed.timestamp()

    def _check_owner_allowed(self, owner: str) -> None:
        if self.allowed_owners and owner.lower() not in self.allowed_owners:
            raise SystemExit(f"owner not allowed by policy: {owner}")

    def installation_token(self, *, owner: str | None = None, repo: str | None = None) -> str:
        if repo:
            parsed_slug = extract_repo_slug(repo)
            if parsed_slug is not None:
                owner_name, repo_name = parsed_slug
                owner = owner_name
                repo = repo_name
            else:
                repo = repo.removesuffix(".git").strip()
                if not repo:
                    raise SystemExit("repo is required")
        if not owner:
            owner = self.default_owner
        if not owner:
            raise SystemExit("owner or repo is required")
        self._check_owner_allowed(owner)

        cache_key = f"{owner}/{repo or '*'}"
        cache = self._load_cache()
        now = time.time()
        cached = cache.get(cache_key)
        if isinstance(cached, dict):
            token = str(cached.get("token", "")).strip()
            expires_at = float(cached.get("expires_at_epoch", 0) or 0)
            if token and expires_at - now > 120:
                return token

        app_jwt = self._build_jwt()
        if repo:
            installation = self._request_json(
                "GET",
                f"/repos/{parse.quote(owner)}/{parse.quote(repo)}/installation",
                bearer=app_jwt,
            )
        else:
            installation = None
            for path in (f"/users/{parse.quote(owner)}/installation", f"/orgs/{parse.quote(owner)}/installation"):
                try:
                    installation = self._request_json("GET", path, bearer=app_jwt)
                    break
                except SystemExit:
                    installation = None
            if installation is None:
                raise SystemExit(f"installation not found for owner: {owner}")
        installation_id = int(installation["id"])
        token_payload = self._request_json(
            "POST",
            f"/app/installations/{installation_id}/access_tokens",
            bearer=app_jwt,
            payload={},
        )
        token = str(token_payload["token"])
        expires_at_epoch = self._parse_expiry(str(token_payload["expires_at"]))
        cache[cache_key] = {"token": token, "expires_at_epoch": expires_at_epoch}
        self._write_cache(cache)
        return token

    def git_credential(self) -> int:
        payload: dict[str, str] = {}
        for line in sys.stdin.read().splitlines():
            if "=" not in line:
                continue
            key, value = line.split("=", 1)
            payload[key.strip()] = value.strip()
        host = payload.get("host", "").strip().lower()
        path = payload.get("path", "").strip().lstrip("/")
        if host != "github.com" or not path:
            return 0
        slug = extract_repo_slug(path)
        if slug is None and "/" not in path:
            repo_name = path.removesuffix(".git").strip()
            owner_name = payload.get("username", "").strip() or self.default_owner
            if owner_name and repo_name:
                slug = (owner_name, repo_name)
        if slug is None:
            return 0
        owner, repo = slug
        token = self.installation_token(owner=owner, repo=repo)
        sys.stdout.write("username=x-access-token\n")
        sys.stdout.write(f"password={token}\n")
        return 0

    def gh_token_from_context(self, *, cwd: str, gh_args: list[str]) -> str:
        repo = self._repo_from_gh_args(gh_args)
        if repo:
            return self.installation_token(repo=repo)
        repo = self._repo_from_git_remote(cwd)
        if repo:
            return self.installation_token(repo=repo)
        if self.default_owner:
            return self.installation_token(owner=self.default_owner)
        raise SystemExit("unable to infer GitHub App installation context for gh command")

    def _repo_from_git_remote(self, cwd: str) -> str | None:
        try:
            proc = subprocess.run(
                ["git", "-C", cwd, "remote", "get-url", "origin"],
                stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL,
                text=True,
                check=False,
                timeout=5,
            )
        except Exception:
            return None
        if proc.returncode != 0:
            return None
        slug = extract_repo_slug(proc.stdout.strip())
        if slug is None:
            return None
        return f"{slug[0]}/{slug[1]}"

    def _repo_from_gh_args(self, gh_args: list[str]) -> str | None:
        for idx, arg in enumerate(gh_args):
            if arg in {"-R", "--repo"} and idx + 1 < len(gh_args):
                return self._coerce_repo_arg(gh_args[idx + 1])
            if arg.startswith("--repo="):
                return self._coerce_repo_arg(arg.split("=", 1)[1])
        return None

    def _coerce_repo_arg(self, value: str) -> str | None:
        slug = extract_repo_slug(value)
        if slug is None and self.default_owner and "/" not in value:
            slug = (self.default_owner, value)
        if slug is None:
            return None
        return f"{slug[0]}/{slug[1]}"


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="GitHub App installation token helper for GoClaw")
    sub = parser.add_subparsers(dest="command", required=True)

    token_cmd = sub.add_parser("token", help="print an installation token")
    token_cmd.add_argument("--repo")
    token_cmd.add_argument("--owner")

    cred_cmd = sub.add_parser("git-credential", help="git credential helper protocol")
    cred_cmd.add_argument("operation", nargs="?", default="get")

    gh_cmd = sub.add_parser("gh-token", help="infer token context for gh wrapper")
    gh_cmd.add_argument("--cwd", default=os.getcwd())
    gh_cmd.add_argument("gh_args", nargs=argparse.REMAINDER)
    return parser


def main() -> int:
    args = build_parser().parse_args()
    helper = GitHubAppHelper()
    if args.command == "git-credential":
        if getattr(args, "operation", "get") in {"store", "erase"}:
            return 0
        return helper.git_credential()
    if args.command == "token":
        token = helper.installation_token(owner=args.owner, repo=args.repo)
        sys.stdout.write(token)
        return 0
    if args.command == "gh-token":
        gh_args = list(args.gh_args)
        if gh_args[:1] == ["--"]:
            gh_args = gh_args[1:]
        token = helper.gh_token_from_context(cwd=args.cwd, gh_args=gh_args)
        sys.stdout.write(token)
        return 0
    raise SystemExit(f"unsupported command: {args.command}")


if __name__ == "__main__":
    raise SystemExit(main())
