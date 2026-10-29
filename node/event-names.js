'use strict'

const aliases = {
  "rotation.verification.skipped": "rotation.verify.skipped",
  "rotation.verification.started": "rotation.verify.started",
  "rotation.verification.failed": "rotation.verify.failed",
  "rotation.verification.cancelled": "rotation.verify.cancelled",
  "application.scope.updated": "app.scope.updated",
  "application.restart.requested": "app.restart.requested",
  "rotation.waiting_for_application": "rotation.waiting_for_app",
  "configuration.updated": "app.scope.updated"
}

module.exports = value => Object.hasOwn(aliases, value) ? aliases[value] : value
