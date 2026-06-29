# Container image for the Opcode42 daemon (plan 13 §"Container image").
#
# goreleaser builds the static, CGO-free `opcoded` binary and supplies it in the
# build context, so this image only needs to copy it into a minimal base. The
# distroless static-debian12:nonroot base ships no shell or package manager and
# runs as an unprivileged user.
#
# Usage (opcoded takes flags directly — there is no `serve` subcommand):
#   docker run -d -p 4096:4096 \
#     -e OPENCODE_SERVER_PASSWORD=secret \
#     ghcr.io/rotemmiz/opcode42:latest --host 0.0.0.0 --port 4096
FROM gcr.io/distroless/static-debian12:nonroot

COPY opcoded /opcoded

# The daemon's only listening port (plan 01 default 4096).
EXPOSE 4096

ENTRYPOINT ["/opcoded"]
