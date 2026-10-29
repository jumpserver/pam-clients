"""Canonical event names shared by stream and command consumers."""

EVENT_NAME_ALIASES = {
    'rotation.verification.skipped': 'rotation.verify.skipped',
    'rotation.verification.started': 'rotation.verify.started',
    'rotation.verification.failed': 'rotation.verify.failed',
    'rotation.verification.cancelled': 'rotation.verify.cancelled',
    'application.scope.updated': 'app.scope.updated',
    'application.restart.requested': 'app.restart.requested',
    'rotation.waiting_for_application': 'rotation.waiting_for_app',
    'configuration.updated': 'app.scope.updated',
}


def canonical_event_name(value):
    return EVENT_NAME_ALIASES.get(value, value) if isinstance(value, str) else value
