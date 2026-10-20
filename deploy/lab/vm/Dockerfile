# syntax=docker/dockerfile:1.7
# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
# A systemd "virtual machine" for testing the agent installer: Debian with Java, curl and sudo.
FROM debian:13-slim
RUN apt-get update && \
    apt-get install -y --no-install-recommends systemd systemd-sysv dbus curl ca-certificates default-jre-headless procps sudo && \
    rm -rf /var/lib/apt/lists/* && \
    find /etc/systemd/system /lib/systemd/system -path '*.wants/*' \( -name '*getty*' -o -name '*udev*' -o -name 'systemd-logind*' \) -delete
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
