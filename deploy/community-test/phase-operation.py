import datetime,json,pathlib,subprocess
out={'at':datetime.datetime.utcnow().isoformat()+'Z','mode':'read-only systemd metadata; no start/stop/restart or config writes','units':{}}
for unit in ['tp-http-scope.service','tp-ha-scope.service','tp-http-scope.timer','tp-ha-scope.timer']:
 args=['systemctl','show',unit,'--property=Id,LoadState,ActiveState,SubState,MainPID,ExecMainPID,InvocationID,ExecMainStartTimestampMonotonic,ExecMainExitTimestampMonotonic,ExecMainStatus,Result,TimersMonotonic,TimersCalendar,AccuracyUSec,RandomizedDelayUSec,LastTriggerUSecMonotonic,NextElapseUSecMonotonic']
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=5)
 out['units'][unit]=dict(line.split('=',1) for line in p.stdout.splitlines() if '=' in line)
out['deployment_lock_inode']=pathlib.Path('/opt/tp-community-actions/deployment.lock').stat().st_ino
print(json.dumps(out))
