#!/bin/sh

set -eu
umask 077

fail() {
	printf '%s\n' "$1" >&2
	exit 1
}

[ "$#" -ge 6 ] || exit 1

launcher_mode="$1"
operation="$2"
target_uid="$3"
target_gid="$4"
target_groups="$5"
shift 5

case "${launcher_mode}" in
	unprivileged | sudo | sudo-launch | sudo-profiled) ;;
	*) fail '[site:network-boundary-bootstrap-mode]' ;;
esac

case "${operation}" in
	profile | churn) ;;
	*) fail '[site:network-boundary-bootstrap-operation]' ;;
esac

case "${target_uid}" in
	'' | 0 | *[!0-9]*) fail '[site:network-boundary-bootstrap-identity]' ;;
esac
case "${target_gid}" in
	'' | 0 | *[!0-9]*) fail '[site:network-boundary-bootstrap-identity]' ;;
esac
case "${target_groups}" in
	'' | *[!0-9,]*) fail '[site:network-boundary-bootstrap-groups]' ;;
esac

validate_sudo_identity() {
	[ "$(/usr/bin/id -u)" -eq 0 ] || fail '[site:network-boundary-bootstrap-privilege]'
	case "${SUDO_UID:-}" in
		'' | 0 | *[!0-9]*) fail '[site:network-boundary-bootstrap-identity]' ;;
	esac
	case "${SUDO_GID:-}" in
		'' | 0 | *[!0-9]*) fail '[site:network-boundary-bootstrap-identity]' ;;
	esac
	[ "${target_uid}" -eq "${SUDO_UID}" ] || fail '[site:network-boundary-bootstrap-identity]'
	[ "${target_gid}" -eq "${SUDO_GID}" ] || fail '[site:network-boundary-bootstrap-identity]'
	account_uid=$(/usr/bin/id -u -- "${SUDO_USER:-}" 2>/dev/null) || fail '[site:network-boundary-bootstrap-identity]'
	account_gid=$(/usr/bin/id -g -- "${SUDO_USER:-}" 2>/dev/null) || fail '[site:network-boundary-bootstrap-identity]'
	account_groups=$(/usr/bin/id -G -- "${SUDO_USER:-}" 2>/dev/null | /usr/bin/tr ' ' '\n' | /usr/bin/sort -nu | /usr/bin/paste -sd, -) || fail '[site:network-boundary-bootstrap-groups]'
	[ "${target_uid}" -eq "${account_uid}" ] || fail '[site:network-boundary-bootstrap-identity]'
	[ "${target_gid}" -eq "${account_gid}" ] || fail '[site:network-boundary-bootstrap-identity]'
	[ "${target_groups}" = "${account_groups}" ] || fail '[site:network-boundary-bootstrap-groups]'
}

if [ "${launcher_mode}" = "sudo-launch" ]; then
	validate_sudo_identity
	exec /usr/bin/unshare \
		--kill-child=SIGTERM \
		--net \
		--fork \
		/bin/sh "$0" sudo "${operation}" "${target_uid}" "${target_gid}" "${target_groups}" "$@"
fi

if [ "${launcher_mode}" = "sudo" ]; then
	validate_sudo_identity
	/usr/sbin/ip link set lo up >/dev/null 2>&1 || fail '[site:network-boundary-bootstrap-loopback]'
	if [ "${operation}" = "churn" ]; then
		/usr/sbin/ip link add ca-boundary0 type dummy >/dev/null 2>&1 || fail '[site:network-boundary-bootstrap-churn]'
		/usr/sbin/ip link set ca-boundary0 up >/dev/null 2>&1 || fail '[site:network-boundary-bootstrap-churn]'
		/usr/sbin/ip link del ca-boundary0 >/dev/null 2>&1 || fail '[site:network-boundary-bootstrap-churn]'
	fi
	[ -x /usr/bin/aa-exec ] || fail '[site:network-boundary-bootstrap-apparmor]'
	[ -r /sys/kernel/security/apparmor/profiles ] || fail '[site:network-boundary-bootstrap-apparmor]'
	/usr/bin/grep -Eq '^chrome \((unconfined|enforce)\)$' /sys/kernel/security/apparmor/profiles || \
		fail '[site:network-boundary-bootstrap-apparmor]'
	exec /usr/bin/aa-exec -p chrome -- \
		/bin/sh "$0" sudo-profiled "${operation}" "${target_uid}" "${target_gid}" "${target_groups}" "$@"
fi

if [ "${launcher_mode}" = "sudo-profiled" ]; then
	validate_sudo_identity
	current_profile=$(/usr/bin/cat /proc/self/attr/current 2>/dev/null) || fail '[site:network-boundary-bootstrap-apparmor]'
	case "${current_profile}" in
		'chrome (unconfined)' | 'chrome (enforce)') ;;
		*) fail '[site:network-boundary-bootstrap-apparmor]' ;;
	esac
	exec /usr/bin/setpriv \
		--reuid "${target_uid}" \
		--regid "${target_gid}" \
		--groups "${target_groups}" \
		--no-new-privs \
		--inh-caps=-all \
		--ambient-caps=-all \
		--bounding-set=-all \
		-- "$@"
fi

[ "$(/usr/bin/id -u)" -eq "${target_uid}" ] || fail '[site:network-boundary-bootstrap-privilege]'
/usr/sbin/ip link set lo up >/dev/null 2>&1 || fail '[site:network-boundary-bootstrap-loopback]'
if [ "${operation}" = "churn" ]; then
	/usr/sbin/ip link add ca-boundary0 type dummy >/dev/null 2>&1 || fail '[site:network-boundary-bootstrap-churn]'
	/usr/sbin/ip link set ca-boundary0 up >/dev/null 2>&1 || fail '[site:network-boundary-bootstrap-churn]'
	/usr/sbin/ip link del ca-boundary0 >/dev/null 2>&1 || fail '[site:network-boundary-bootstrap-churn]'
fi

# A single-ID rootless mapping cannot rewrite supplementary groups because
# setgroups(2) is deliberately disabled. Keep the inherited unmapped entries;
# the verified child rejects group 0 and all capabilities before Node runs.
exec /usr/bin/setpriv \
	--reuid "${target_uid}" \
	--regid "${target_gid}" \
	--keep-groups \
	--no-new-privs \
	--inh-caps=-all \
	--ambient-caps=-all \
	--bounding-set=-all \
	-- "$@"
