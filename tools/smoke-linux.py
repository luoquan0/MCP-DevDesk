#!/usr/bin/env python3
"""Native Linux acceptance tests. Only temporary copies receive runtime data."""
import http.cookiejar
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request


def require(value, message):
    if not value:
        raise AssertionError(message)


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def http(url, method='GET', data=None, headers=None, opener=None, expected=200):
    h = dict(headers or {})
    body = None
    if data is not None:
        body = json.dumps(data).encode()
        h['Content-Type'] = 'application/json'
    req = urllib.request.Request(url, data=body, method=method, headers=h)
    try:
        response = (opener.open(req, timeout=40) if opener else urllib.request.urlopen(req, timeout=40))
    except urllib.error.HTTPError as err:
        response = err
    with response:
        raw = response.read()
        require(response.status == expected, f'{method} {url} returned {response.status}, expected {expected}: {raw[:1000]!r}')
        try:
            value = json.loads(raw)
        except (ValueError, UnicodeDecodeError):
            value = raw.decode(errors='replace')
        return value, response.headers


def ready(url, process):
    for _ in range(100):
        require(process.poll() is None, f'process exited before readiness: {process.returncode}')
        try:
            return http(url)[0]
        except (OSError, AssertionError):
            time.sleep(.1)
    raise AssertionError('readiness timeout: '+url)


def stopped(pid):
    try:
        text = Path(f'/proc/{pid}/stat').read_text()
        return text[text.rfind(')')+2:].split()[0] == 'Z'
    except FileNotFoundError:
        return True


def stop_process(process):
    if process is None or process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(15)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(5)
        raise AssertionError('graceful server shutdown timed out')
    require(process.returncode == 0, f'graceful server shutdown failed: {process.returncode}')


class RPC:
    def __init__(self, base):
        self.base, self.session, self.seq = base, '', 0
        value, headers = self.send('initialize', {'protocolVersion':'2025-06-18','capabilities':{},'clientInfo':{'name':'linux-native-smoke','version':'1'}})
        self.session = headers.get('Mcp-Session-Id', '')
        require(self.session, 'initialize did not return session ID')
        require('linux' in value['serverInfo']['version'], 'compiled core does not report Linux preview version')
        self.send('notifications/initialized', None, notification=True)

    def send(self, method, params=None, notification=False):
        headers = {'Accept':'application/json, text/event-stream','MCP-Protocol-Version':'2025-06-18'}
        if self.session:
            headers['Mcp-Session-Id'] = self.session
        payload = {'jsonrpc':'2.0','method':method}
        if params is not None:
            payload['params'] = params
        if not notification:
            self.seq += 1
            payload['id'] = self.seq
        value, response_headers = http(self.base+'/mcp','POST',payload,headers,expected=202 if notification else 200)
        if notification:
            return value, response_headers
        require(isinstance(value,dict) and 'error' not in value, f'JSON-RPC failure: {value}')
        return value['result'], response_headers

    def tool(self, name, arguments=None):
        result, _ = self.send('tools/call', {'name':name,'arguments':arguments or {}})
        require(not result.get('isError'), f'{name}: {result}')
        require('structuredContent' in result, f'{name}: no structured result')
        return result['structuredContent']

    def finish(self, value, timeout=100):
        deadline = time.monotonic()+timeout
        while value.get('running') and time.monotonic()<deadline:
            time.sleep(.1)
            value = self.tool('read_output', {'sessionId':value['sessionId'],'offset':0})
        require(not value.get('running'), f'command did not finish: {value}')
        return value


def core_smoke(stage, root):
    workspace = root/'workspace'; workspace.mkdir()
    (workspace/'go.mod').write_text('module example.test/linuxsmoke\n\ngo 1.23\n')
    (workspace/'sample.go').write_text('package sample\n\ntype Widget struct{}\n\nfunc BuildWidget() *Widget { return &Widget{} }\n\nfunc UseWidget() *Widget { return BuildWidget() }\n')
    node = workspace/'node'; node.mkdir()
    (node/'package.json').write_text(json.dumps({'name':'linux-smoke','version':'1.0.0','scripts':{name:'node -e "process.stdout.write(\'OK\\n\')"' for name in ('test','check','lint','build')}}))
    base = 'http://127.0.0.1:'+str(free_port())
    process = None
    with (root/'core.log').open('wb') as log:
        try:
            process = subprocess.Popen([str(stage/'mcp-core'),'--workspace',str(workspace),'--port',base.rsplit(':',1)[1],'--permission-mode','trusted','--allow-network','--tool-profile','full','--data-dir',str(root/'core-data')],stdout=log,stderr=log)
            ready(base+'/healthz',process)
            rpc = RPC(base)
            tools = rpc.send('tools/list',{})[0]['tools']
            names = {x['name'] for x in tools}
            required = {'validate_project','checks_run','exec_command','read_output','write_stdin','kill_session','document_symbols','workspace_symbols','find_definition','find_references'}
            require(required <= names, 'missing tools: '+str(required-names))
            require(not any(n.startswith('task_') for n in names), 'AI Task tools leaked into Linux preview')
            symbols = rpc.tool('document_symbols', {'path':'sample.go'})
            require(symbols['engine']=='lexical-fallback' and symbols['count']>=3, 'document_symbols failed')
            require(rpc.tool('workspace_symbols',{'query':'Widget'})['count']>=2,'workspace_symbols failed')
            require(rpc.tool('find_definition',{'symbol':'BuildWidget'})['count']==1,'find_definition failed')
            require(rpc.tool('find_references',{'symbol':'BuildWidget'})['count']>=2,'find_references failed')
            checks = rpc.tool('checks_run',{'type':'test','waitMillis':30000})
            require(checks.get('detectedRuntime')=='go','Go checks_run detection failed')
            require(rpc.finish(checks).get('exitCode')==0,'Go checks_run failed')
            validation = rpc.tool('validate_project',{'waitMillis':30000})
            require(validation.get('validationPhases')==3,'Go validation phases failed')
            require(rpc.finish(validation).get('exitCode')==0,'Go validate_project failed')
            validation = rpc.tool('validate_project',{'cwd':'node','waitMillis':30000})
            require(rpc.finish(validation).get('exitCode')==0,'Node validate_project failed')
            script = 'printf "READY\\n"; IFS= read -r line; printf "ECHO:%s\\n" "$line"; sleep 300 & printf "CHILD:%s\\n" "$!"; wait'
            terminal = rpc.tool('exec_command',{'command':'/bin/sh','args':['-c',script],'waitMillis':100})
            sid = terminal['sessionId']; require(terminal['running'],'terminal is not running')
            rpc.tool('write_stdin',{'sessionId':sid,'chars':'Linux中文✓\n'})
            output = ''
            for _ in range(60):
                output = rpc.tool('read_output',{'sessionId':sid,'offset':0})['output']
                if 'CHILD:' in output:
                    break
                time.sleep(.1)
            require('ECHO:Linux中文✓' in output,'UTF-8 stdin/output mismatch')
            child = int(re.search(r'CHILD:(\d+)',output).group(1))
            require(not stopped(child),'child was not running before kill')
            killed = rpc.tool('kill_session',{'sessionId':sid,'wait_ms':5000})
            require(killed.get('terminated') and killed.get('completed'),'kill_session failed')
            require(not rpc.tool('read_output',{'sessionId':sid}).get('running'),'read_output.running remains true')
            require(stopped(child),'child process survived process-group kill')
            killed = rpc.tool('kill_session',{'sessionId':sid})
            require(not killed.get('terminated') and killed.get('completed'),'repeated kill is not idempotent')
            timeout = rpc.tool('exec_command',{'command':'/bin/sh','args':['-c','sleep 300 & echo TIME_CHILD:$!; wait'],'timeoutSeconds':1,'waitMillis':100})
            timeout = rpc.finish(timeout,10)
            match = re.search(r'TIME_CHILD:(\d+)',timeout['output'])
            require(match and stopped(int(match.group(1))),'timeout left a child process running')
            print('PASS compiled Linux Core: JSON-RPC, Go/Node validation, lexical navigation, UTF-8 stdin/output, process-tree kill, timeout and idempotence')
        finally:
            stop_process(process)


def manager_smoke(stage, root):
    install = root/'install'; shutil.copytree(stage,install)
    env = os.environ.copy()
    password = secrets.token_urlsafe(24)
    env['MCP_DEVDESK_WEB_PASSWORD'] = password
    env['MCP_DEVDESK_KEY_FILE'] = str(install/'data/devdesk/master.key')
    web_port, admin_port, mcp_port = free_port(), free_port(), free_port()
    subprocess.run([str(install/'mcp-devdesk'),'--root',str(install),'--init','--web-port',str(web_port)],env=env,check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    cfg_path = install/'data/devdesk/config.json'
    cfg = json.loads(cfg_path.read_text()); cfg['adminPort']=admin_port; cfg['mcpPort']=mcp_port
    cfg_path.write_text(json.dumps(cfg)); cfg_path.chmod(0o600)
    envelope = (install/'data/devdesk/secrets.json').read_text()
    require('linux-aes256-gcm-v1' in envelope and password not in envelope,'server password is not encrypted')
    require((install/'data/devdesk/master.key').stat().st_mode & 0o777 == 0o600,'master key permissions failed')
    env.pop('MCP_DEVDESK_WEB_PASSWORD')
    base = f'http://127.0.0.1:{web_port}'
    process = None
    with (root/'manager.log').open('wb') as log:
        try:
            process = subprocess.Popen([str(install/'mcp-devdesk'),'--root',str(install)],env=env,stdout=log,stderr=log)
            ready(base+'/api/control/auth/status',process)
            http(base+'/api/status',expected=401)
            http(base+'/api/control/auth/login','POST',{'password':'incorrect'},expected=401)
            jar = http.cookiejar.CookieJar(); opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
            http(base+'/api/control/auth/login','POST',{'password':password},opener=opener)
            status = http(base+'/api/status',opener=opener)[0]
            require('linux' in status['version'],'manager version does not identify Linux preview')
            page = http(base+'/',opener=opener)[0]
            require(isinstance(page,str) and '<html' in page.lower(),'embedded Vue entrypoint missing')
            for asset in re.findall(r'(?:src|href)="([^"]+\.(?:js|css))"',page):
                http(base+'/'+asset.lstrip('/'),opener=opener)
            http(base+'/api/projects',opener=opener)
            http(base+'/api/instances',opener=opener)
            http(base+'/api/tunnels/processes',opener=opener)
            http(base+'/api/control/directories',opener=opener)
            http(base+'/api/secrets',opener=opener,expected=403)
            http(base+'/api/services/start','POST',{},headers={'Origin':'http://untrusted.example'},opener=opener,expected=403)
            http(base+'/api/services/start','POST',{},opener=opener)
            status = http(base+'/api/status',opener=opener)[0]
            require(status['mcp']['running'],'manager failed to start real MCP child')
            http(f'http://127.0.0.1:{mcp_port}/healthz')
            http(base+'/api/services/stop','POST',{},opener=opener)
            for _ in range(50):
                status = http(base+'/api/status',opener=opener)[0]
                if not status['mcp']['running']:
                    break
                time.sleep(.1)
            require(not status['mcp']['running'],'manager failed to stop MCP child')
            # The installation lock must also prevent an in-place upgrade.
            locked = subprocess.run(['bash',str(stage/'install-linux.sh'),str(install)],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
            require(locked.returncode != 0,'installer upgraded a running service')
            print('PASS compiled Linux Manager: init, encryption, authenticated Web UI, APIs, Origin guard, real MCP start/stop and installation lock')
        finally:
            stop_process(process)
    # Preserve an independently updated cloudflared byte-for-byte across upgrades.
    sentinel = b'independently-updated-cloudflared-test'
    (install/'cloudflared').write_bytes(sentinel)
    before_config = cfg_path.read_bytes()
    before_key = (install/'data/devdesk/master.key').read_bytes()
    subprocess.run(['bash',str(stage/'install-linux.sh'),str(install)],check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    require((install/'cloudflared').read_bytes()==sentinel,'Linux upgrade overwrote cloudflared')
    require(cfg_path.read_bytes()==before_config,'Linux upgrade changed user config')
    require((install/'data/devdesk/master.key').read_bytes()==before_key,'Linux upgrade changed encryption key')
    fresh = root/'fresh'
    subprocess.run(['bash',str(stage/'install-linux.sh'),str(fresh)],check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    require((fresh/'cloudflared').read_bytes()==(stage/'cloudflared').read_bytes(),'fresh install failed to seed cloudflared')
    unit = subprocess.check_output(['bash',str(fresh/'install-service.sh'),'--print'],text=True)
    require('KillMode=control-group' in unit and 'ExecStart=' in unit,'systemd unit missing safe lifecycle settings')
    print('PASS Linux installer: existing cloudflared/config/master key preserved; fresh install seeds runtime; systemd user unit generated')


def main():
    stage = Path(sys.argv[1]).resolve()
    with tempfile.TemporaryDirectory(prefix='devdesk-linux-smoke-') as temp:
        root = Path(temp)
        core_smoke(stage,root)
        manager_smoke(stage,root)
    print('ALL LINUX BINARY ACCEPTANCE TESTS PASSED')


if __name__ == '__main__':
    main()
