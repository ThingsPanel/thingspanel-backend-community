"""Restore the prior service configuration if a handled rollout step fails.

Schema changes are not reversed. Only backward-compatible migrations may use
this guard; each database migration batch must remain transactional.
"""
from contextlib import contextmanager
import os
import pathlib


@contextmanager
def restore_services_on_failure(compose, services, run):
    compose = pathlib.Path(compose)
    previous = compose.read_bytes()
    try:
        yield
    except BaseException:
        recovery = compose.with_name(compose.name + '.recovery')
        recovery.write_bytes(previous)
        os.chmod(str(recovery), 0o600)
        os.replace(str(recovery), str(compose))
        try:
            run(['docker', 'compose', '-f', str(compose), 'up', '-d',
                 '--no-deps', '--no-build', '--pull', 'never'] + list(services))
        except Exception as recovery_error:
            raise RuntimeError('Deployment failed; prior configuration restored but service recovery also failed') from recovery_error
        raise
