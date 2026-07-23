# Production build script for the opcode42-builder E2B template.
#
# Usage:
#   pip install e2b python-dotenv
#   echo 'E2B_API_KEY=e2b_***' > .env
#   python e2b/build_prod.py
#
# After the build, set E2B_TEMPLATE=opcode42-builder as a GitHub Actions secret.
from __future__ import annotations

from dotenv import load_dotenv
from e2b import Template, default_build_logger

from template import template

load_dotenv()

if __name__ == "__main__":
    Template.build(
        template,
        "opcode42-builder",
        cpu_count=2,
        memory_mb=2048,
        on_build_logs=default_build_logger(),
    )