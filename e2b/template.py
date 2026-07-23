# E2B template for the agentic-devex worker sandbox.
#
# Built with the E2B V2 fluent Template() builder (SDK >= 2.3.0).
# Bake:  python e2b/build_prod.py
# Boots: <200ms, full toolchain ready, no apt install at runtime.
#
# Contains: Ubuntu 24.04 + Go 1.26 + Node 20 + Bun + gh CLI + golangci-lint
#           + asciinema + gitleaks + opencode (the agent runtime).
#
# The sandbox does NOT set a start_cmd — the worker.py starts opencode serve
# explicitly (and kills it before the gate, then restarts it for the preview
# URL). A baked start_cmd would conflict with that lifecycle.
#
# All run_cmd calls use user='root' because the E2B build runs as a non-root
# user by default, and system-level installs (apt, tar -C /usr/local, etc.)
# need root.
from __future__ import annotations

from e2b import Template

GO_VERSION = "1.26.3"
NODE_MAJOR = "20"

template = (
    Template()
    # Ubuntu 24.04 base — has apt, curl, ca-certificates, git
    .from_ubuntu_image("24.04")
    # System packages
    .apt_install([
        "ca-certificates",
        "curl",
        "git",
        "make",
        "build-essential",
        "asciinema",
        "psmisc",           # provides fuser — for fuser -k 4096/tcp (kill the agent server before the gate)
        "unzip",            # required by bun.sh/install
    ])
    # Go toolchain — extract to /usr/local/go (needs root)
    .run_cmd(
        f"curl -sSL https://go.dev/dl/go{GO_VERSION}.linux-amd64.tar.gz "
        f"| tar -C /usr/local -xz",
        user="root",
    )
    # Set PATH system-wide so the runtime 'user' (not root) can find go, bun,
    # and opencode. E2B's set_envs only applies during build, not at runtime.
    .run_cmd(
        'echo "PATH=/usr/local/go/bin:/usr/local/.bun/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin" > /etc/environment',
        user="root",
    )
    .run_cmd(
        'echo "export PATH=/usr/local/go/bin:/usr/local/.bun/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin" > /etc/profile.d/opencode-path.sh',
        user="root",
    )
    # gh CLI (for branch-pusher to open PRs inside the sandbox — needs root for apt)
    .run_cmd([
        "curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg "
        "-o /usr/share/keyrings/githubcli-archive-keyring.gpg",
        "echo 'deb [arch=amd64 signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] "
        "https://cli.github.com/packages stable main' > /etc/apt/sources.list.d/github-cli.list",
        "apt-get update && apt-get install -y gh",
    ], user="root")
    # golangci-lint — pin v2.12.2 and download directly (the install.sh script
    # has a checksum-verification bug in some environments; direct download
    # with a pinned version is more reliable)
    .run_cmd(
        "curl -sSL https://github.com/golangci/golangci-lint/releases/download/"
        "v2.12.2/golangci-lint-2.12.2-linux-amd64.tar.gz "
        "| tar -xz -C /usr/local/bin --strip-components=1 "
        "golangci-lint-2.12.2-linux-amd64/golangci-lint",
        user="root",
    )
    # gitleaks (secret scanner — used in the gate). Pin a version — the
    # "latest/download/" path serves versioned filenames that don't match the
    # versionless URL pattern, and the asset name uses lowercase "linux".
    .run_cmd(
        "curl -sSL https://github.com/gitleaks/gitleaks/releases/download/"
        "v8.30.1/gitleaks_8.30.1_linux_x64.tar.gz "
        "| tar -xz -C /usr/local/bin gitleaks",
        user="root",
    )
    # Node.js 20 (opencode is a Bun/Node app — needs the runtime; apt needs root)
    .run_cmd([
        f"curl -fsSL https://deb.nodesource.com/setup_{NODE_MAJOR}.x | bash -",
        f"apt-get install -y nodejs",
    ], user="root")
    # Bun (opencode's runtime). Install to /usr/local/.bun (shared, accessible
    # by the runtime 'user' — /root is not accessible at runtime).
    .run_cmd(
        "export BUN_INSTALL=/usr/local/.bun && "
        "curl -fsSL https://bun.sh/install | bash && "
        "chmod -R a+rX /usr/local/.bun",
        user="root",
    )
    # opencode (the agent runtime — the thing worker.py drives via HTTP).
    # https://opencode.ai/install (307 → raw.githubusercontent.com/.../install).
    # Installs to /root/.opencode/bin/opencode. The runtime 'user' can't read
    # /root, so chmod the path and symlink to /usr/local/bin.
    .run_cmd([
        "curl -fsSL https://opencode.ai/install | bash",
        "chmod a+rx /root /root/.opencode /root/.opencode/bin",
        "rm -f /usr/local/bin/opencode",
        "ln -s /root/.opencode/bin/opencode /usr/local/bin/opencode",
    ], user="root")
    # Verify opencode actually runs (fails the bake if Bun/opencode is broken)
    .run_cmd("opencode --version", user="root")
)