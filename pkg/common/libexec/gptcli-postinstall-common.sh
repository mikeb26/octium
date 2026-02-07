#!/bin/sh
set -eu

# Common post-install helpers intended to be called from package maintainer
# scripts (deb postinst, rpm %post, etc.).

log() {
	# Prefer stderr so the message shows up in package-manager logs.
	echo "gptcli: $*" >&2
}

is_noninteractive() {
	# A best-effort check.
	# - DEBIAN_FRONTEND=noninteractive is common for apt/dpkg.
	# - no TTY usually means CI/noninteractive installs.
	if [ "${DEBIAN_FRONTEND:-}" = "noninteractive" ]; then
		return 0
	fi
	if [ ! -t 0 ] || [ ! -t 1 ]; then
		return 0
	fi
	return 1
}

invoking_user() {
	# Maintainer scripts run as root. Try to infer the human invoker.
	# 1) sudo installs: $SUDO_USER
	# 2) otherwise: logname (may fail in some CI contexts)
	if [ -n "${SUDO_USER:-}" ] && [ "${SUDO_USER}" != "root" ]; then
		echo "${SUDO_USER}"
		return 0
	fi

	# logname exits non-zero if no login name is associated with the session.
	if command -v logname >/dev/null 2>&1; then
		u="$(logname 2>/dev/null || true)"
		if [ -n "${u}" ] && [ "${u}" != "root" ]; then
			echo "${u}"
			return 0
		fi
	fi

	return 1
}

ensure_group_exists() {
	group="$1"
	if getent group "${group}" >/dev/null 2>&1; then
		return 0
	fi
	if command -v groupadd >/dev/null 2>&1; then
		# Use a system group if possible.
		groupadd --system "${group}" 2>/dev/null || groupadd "${group}" || return 1
		return 0
	fi
	return 1
}

maybe_add_invoker_to_group() {
	group="$1"

	user=""
	if user="$(invoking_user)"; then
		:
	else
		log "unable to determine invoking user; not auto-adding anyone to '${group}'."
		log "to enroll a user later: sudo usermod -a -G ${group} <username>"
		return 0
	fi

	if ! id "${user}" >/dev/null 2>&1; then
		log "invoking user '${user}' does not exist; not adding to '${group}'."
		return 0
	fi

	# If already a member, do nothing.
	if id -nG "${user}" 2>/dev/null | tr ' ' '\n' | grep -qx "${group}"; then
		return 0
	fi

	if is_noninteractive; then
		log "non-interactive install detected; automatically adding '${user}' to '${group}'."
	else
		echo "Add '${user}' to group '${group}' now?" >&2
		printf "(This enables gptcli shared workspace access and related permissions.) [Y/n]: " >&2
		read -r ans || ans=""
		case "${ans}" in
			n|N|no|NO)
				log "not adding '${user}' to '${group}' (user declined)."
				log "to enroll later: sudo usermod -a -G ${group} ${user}"
				return 0
				;;
		esac
	fi

	if ! ensure_group_exists "${group}"; then
		log "failed to ensure group '${group}' exists; cannot enroll '${user}'."
		return 0
	fi

	if command -v usermod >/dev/null 2>&1; then
		usermod -a -G "${group}" "${user}" || {
			log "failed to add '${user}' to '${group}'."
			return 0
		}
		log "added '${user}' to '${group}'."
		log "note: you may need to log out/in (or restart your session) for group membership to apply."
		return 0
	fi

	log "'usermod' not available; cannot add '${user}' to '${group}'."
	return 0
}

ensure_sudoers_dropin_perms() {
	dropin="$1"

	# If sudo isn't installed, the dir may still exist (created by packaging), but
	# ensure it's at least present.
	if [ ! -d /etc/sudoers.d ]; then
		mkdir -p /etc/sudoers.d
	fi

	# sudo on Debian typically wants 0750; 0755 is also commonly accepted.
	chmod 0750 /etc/sudoers.d 2>/dev/null || chmod 0755 /etc/sudoers.d || true
	chown root:root /etc/sudoers.d 2>/dev/null || true

	if [ -f "${dropin}" ]; then
		chown root:root "${dropin}" 2>/dev/null || true
		chmod 0440 "${dropin}" 2>/dev/null || true
	fi
}

ensure_root_executable() {
	path="$1"
	if [ -f "${path}" ]; then
		chown root:root "${path}" 2>/dev/null || true
		chmod 0755 "${path}" 2>/dev/null || true
	fi
}
