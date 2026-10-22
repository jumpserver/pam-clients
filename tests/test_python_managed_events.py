"""Managed events keep business application separate from delivery and telemetry."""
import sys
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'python'))
from jms_pam import Client
from jms_pam._event_dispatcher import EventDispatcher


class ApplicationClient(Client):
    def on_account_changed(self, account):
        pass


class ManagedAccountEventTests(unittest.TestCase):
    def setUp(self):
        self.client = ApplicationClient('https://example.com', app_id='app', app_secret='secret', instance_id='worker')
        self.addCleanup(self.client.close)
        self.client.get_account = Mock(return_value=SimpleNamespace(id='account-a', revision=2))
        self.client.confirm_event = Mock()
        self.client.report_accounts = Mock()
        self.client.on_account_changed = Mock()
        self.client.on_account_revoked = Mock()
        self.client.on_event_error = Mock()
        self.client.report_application_command_result = Mock(return_value=SimpleNamespace(accepted=True))
        self.dispatcher = EventDispatcher(self.client)
        self.update = {'event': 'credential.updated', 'event_id': 'event-a', 'account_id': 'account-a',
                       'account_revision': 2, 'revision': 3, 'credential_mode': 'alternating_rotation', 'key': 'database'}

    def test_callback_finishes_before_success_and_failed_ack_does_not_reapply(self):
        order = []
        self.client.on_account_changed.side_effect = lambda account: order.append('applied')
        def confirm(**kwargs):
            order.append(kwargs['status'])
            if len(order) == 2:
                raise OSError('backend unavailable')
        self.client.confirm_event.side_effect = confirm
        self.dispatcher._dispatch(self.update)
        self.assertEqual(order, ['applied', 'success'])
        self.assertTrue(self.dispatcher.pending)
        self.dispatcher._refresh(self.update, self.update)
        self.assertEqual(order, ['applied', 'success', 'success'])
        self.assertFalse(self.dispatcher.pending)
        self.client.on_account_changed.assert_called_once()
        self.client.get_account.assert_called_once_with(account_id='account-a', allow_local_fallback=False)

    def test_business_failure_reports_failed_and_retries(self):
        self.client.on_account_changed.side_effect = [RuntimeError('cannot connect'), None]
        self.dispatcher._dispatch(self.update)
        self.assertEqual(self.client.confirm_event.call_args.kwargs['status'], 'failed')
        self.dispatcher._refresh(self.update, self.update)
        self.assertEqual(self.client.confirm_event.call_args.kwargs['status'], 'success')
        self.assertEqual(self.client.on_account_changed.call_count, 2)

    def test_wrong_account_or_revision_never_reaches_business_callback(self):
        for account in (SimpleNamespace(id='account-b', revision=2), SimpleNamespace(id='account-a', revision=3)):
            self.client.get_account.return_value = account
            self.dispatcher._dispatch(self.update)
            self.client.on_account_changed.assert_not_called()
            self.assertEqual(self.client.confirm_event.call_args.kwargs['status'], 'failed')

    def test_rotation_usage_tracks_current_account_and_reconnect_is_idempotent(self):
        self.dispatcher._dispatch({'event': 'snapshot', 'credentials': [self.update]})
        self.dispatcher._dispatch({'event': 'snapshot', 'credentials': [self.update]})
        self.client.on_account_changed.assert_called_once()
        self.client.get_account.return_value = SimpleNamespace(id='account-b', revision=4)
        self.dispatcher._dispatch({**self.update, 'event_id': 'event-b', 'account_id': 'account-b', 'account_revision': 4, 'revision': 4})
        self.client.report_accounts.assert_called_with(['account-b'])
        self.assertEqual(self.client.on_account_changed.call_count, 2)

    def test_revocation_releases_account_and_regrant_applies_it_again(self):
        self.dispatcher._dispatch(self.update)
        self.dispatcher._dispatch({'event': 'snapshot', 'credentials': []})
        self.client.on_account_revoked.assert_called_once_with('account-a')
        self.client.report_accounts.assert_called_with([])
        self.dispatcher._dispatch(self.update)
        self.client.on_account_changed.assert_called_once()
        self.dispatcher._dispatch({'event': 'snapshot', 'credentials': [self.update]})
        self.assertEqual(self.client.on_account_changed.call_count, 2)

    def test_usage_failure_does_not_repeat_successful_application(self):
        self.client.report_accounts.side_effect = OSError('telemetry unavailable')
        self.dispatcher._dispatch(self.update)
        self.assertFalse(self.dispatcher.pending)
        self.client.on_account_changed.assert_called_once()

    def test_rejected_or_unknown_commands_do_not_execute(self):
        self.client.report_application_command_result.return_value.accepted = False
        self.dispatcher._dispatch({**self.update, 'command_id': 'command'})
        self.client.on_account_changed.assert_not_called()
        self.client.confirm_event.assert_not_called()
        self.client.report_application_command_result.reset_mock()
        self.dispatcher._dispatch({**self.update, 'command_id': 'command', 'event': 'future.command'})
        self.client.report_application_command_result.assert_not_called()

    def test_failed_command_is_reported_without_repeating_action(self):
        self.client.on_account_changed.side_effect = RuntimeError('cannot apply')
        update = {**self.update, 'command_id': 'command'}
        self.dispatcher._dispatch(update)
        self.dispatcher._refresh(update, update)
        self.client.on_account_changed.assert_called_once()
        self.client.confirm_event.assert_called_once_with(event_id='event-a', status='failed', error_code='application_failed')


if __name__ == '__main__':
    unittest.main()
