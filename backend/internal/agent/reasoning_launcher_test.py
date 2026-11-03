"""Integration tests against bundled Hermes transports; no network or SDK patches."""
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock
import reasoning_launcher as bridge
from providers import get_provider_profile, register_provider
from hermes_constants import parse_reasoning_effort
from agent.transports.chat_completions import ChatCompletionsTransport
from agent.transports.codex import ResponsesApiTransport


class NativeReasoningTests(unittest.TestCase):
    def setUp(self):
        original = get_provider_profile('custom')
        self.addCleanup(register_provider, original)

    def configure(self, model, base, mode='chat_completions', values=None, wire=None, metadata=None):
        self.config = dict(model=model, base_url=base, api_mode=mode)
        supplement = None
        if values:
            supplement = dict(options=[dict(value=v, label=v) for v in values], default=values[-1], source='official-test', wire=wire or '')
        self.cap = bridge.describe(self.config, metadata, supplement)
        self.profile = bridge.install_profile(self.config, self.cap)
        return [o['value'] for o in self.cap['options']]

    def request(self, effort, **overrides):
        params = dict(provider='custom', provider_profile=self.profile, base_url=self.config['base_url'], reasoning_config=parse_reasoning_effort(effort))
        params.update(overrides)
        transport = ResponsesApiTransport() if self.config['api_mode'] == 'codex_responses' else ChatCompletionsTransport()
        return transport.build_kwargs(self.config['model'], [{'role':'user','content':'hi'}], **params)

    def test_describe_pipes_keep_utf8_chinese_labels(self):
        # Windows pipes decode stdin with the ANSI code page (GBK) unless the
        # launcher reconfigures stdio; the Go host always writes UTF-8.
        request = {"config": {"model": "m", "base_url": "https://x.example/v1", "api_mode": "chat_completions"},
                   "models": [{"model": "m", "supplement": {"options": [{"value": "low", "label": "低"}, {"value": "max", "label": "最大"}],
                                                            "default": "max", "source": "official", "wire": "openai_chat"}}]}
        env = {k: v for k, v in os.environ.items() if k not in ("PYTHONUTF8", "PYTHONIOENCODING")}
        script = "import sys; sys.argv = ['reasoning_launcher', 'describe']; import reasoning_launcher; reasoning_launcher.main()"
        run = subprocess.run([sys.executable, "-c", script], input=json.dumps(request).encode("utf-8"),
                             capture_output=True, env=env,
                             cwd=os.path.dirname(os.path.abspath(bridge.__file__)))
        self.assertEqual(run.returncode, 0, run.stderr.decode("utf-8", "replace"))
        labels = {o["value"]: o["label"] for o in json.loads(run.stdout.decode("utf-8"))["m"]["options"]}
        self.assertEqual(labels, {"low": "低", "max": "最大"})

    def test_deepseek_alias_native_translation(self):
        self.assertEqual(self.configure('deepseek-chat', 'https://api.deepseek.com/v1'), ['none','low','medium','high','max'])
        body = self.request('max')
        self.assertEqual(body['reasoning_effort'], 'max')
        self.assertEqual(body['extra_body']['thinking'], {'type':'enabled'})
        self.assertEqual(self.request('none')['extra_body']['thinking'], {'type':'disabled'})

    def test_kimi_official_controls(self):
        self.configure('kimi-k3', 'https://api.moonshot.cn/v1', values=['low','high','max'], wire='openai_chat')
        for value in ['low','high','max']:
            body = self.request(value)
            self.assertEqual(body['reasoning_effort'], value)
            self.assertNotIn('thinking', body.get('extra_body', {}))
        self.configure('kimi-k2.6', 'https://api.moonshot.cn/v1', values=['none','enabled'], wire='thinking_toggle')
        for value, enabled in [('none',False),('medium',True)]:
            body = self.request(value)
            self.assertNotIn('reasoning_effort', body)
            self.assertEqual(body['extra_body']['thinking']['type'], 'enabled' if enabled else 'disabled')

    def test_minimax_chat_controls_cn_and_global(self):
        for host in ['api.minimaxi.com', 'api.minimax.io']:
            self.configure('MiniMax-M3.1-Flash-Preview', 'https://' + host + '/v1', values=['low','medium','high','xhigh','max'], wire='minimax')
            for value in ['low','medium','high','xhigh','max']:
                body = self.request(value)
                self.assertEqual(body['reasoning_effort'], value)
                self.assertEqual(body['extra_body']['thinking']['type'], 'adaptive')
            self.configure('MiniMax-M3', 'https://' + host + '/v1', values=['none','enabled'], wire='minimax')
            self.assertEqual(self.request('none')['extra_body']['thinking']['type'], 'disabled')
            self.assertNotIn('reasoning_effort', self.request('medium'))

    def test_siliconflow_budget(self):
        self.configure('Qwen/Qwen3-8B', 'https://api.siliconflow.cn/v1', values=['none','low','medium','high'], wire='siliconflow_budget')
        for value, budget in [('low',1024),('medium',4096),('high',8192)]:
            body = self.request(value)
            self.assertEqual(body['extra_body'], {'enable_thinking':True, 'thinking_budget':budget})
            self.assertNotIn('reasoning_effort', body)
        self.assertEqual(self.request('none')['extra_body'], {'enable_thinking':False})

    def test_native_responses_provider_matrix(self):
        for model, host, values in [
            ('deepseek-flash','api.deepseek.com',['none','low','high','max']),
            ('kimi-k3','api.moonshot.cn',['low','high','max']),
            ('MiniMax-M3.1-Flash-Preview','api.minimaxi.com',['low','medium','high','xhigh','max']),
            ('qwen3.8-max','workspace.cn-beijing.maas.aliyuncs.com',['none','low','medium','xhigh']),
            ('gpt-6.1-sol','api.openai.com',['low','medium','high','xhigh','max']),
            ('vendor-model','custom.example',['low','high']),
        ]:
            self.configure(model,'https://' + host + '/v1','codex_responses',values=values,wire='openai_responses')
            for value in values:
                overrides = {'request_overrides':{'extra_body':{'reasoning':{'effort':'none'}}}} if value == 'none' else {}
                body = self.request(value, **overrides)
                reasoning = body.get('extra_body',{}).get('reasoning',body.get('reasoning',{}))
                self.assertEqual(reasoning.get('effort'), value, (model, value, body))
                self.assertNotIn('reasoning_effort',body)
        self.configure('MiniMax-M3','https://api.minimaxi.com/v1','codex_responses',values=['none','enabled'],wire='openai_responses')
        self.assertEqual(self.request('medium')['reasoning']['effort'],'medium')

    def test_anthropic_native_request(self):
        from agent.transports.anthropic import AnthropicTransport
        for model, values, wire in [
            ('claude-opus-5-5',['low','medium','high','xhigh','max'],'anthropic'),
            ('claude-sonnet-4-5',['none','low','medium','high'],'anthropic_budget'),
        ]:
            actual = self.configure(model,'https://api.anthropic.com','anthropic_messages',values=values,wire=wire)
            self.assertEqual(actual, values)
            for value in values:
                body = AnthropicTransport().build_kwargs(model,[{'role':'user','content':'hi'}],reasoning_config=parse_reasoning_effort(value))
                if wire == 'anthropic':
                    self.assertEqual(body['output_config']['effort'],value)
                    self.assertEqual(body['thinking']['type'],'adaptive')
                elif value == 'none':
                    self.assertNotIn('thinking',body)
                else:
                    self.assertEqual(body['thinking']['budget_tokens'],{'low':4000,'medium':8000,'high':16000}[value])

    def test_glm_documented_levels_use_native_wire(self):
        self.configure('glm-5.3-flash','https://open.bigmodel.cn/api/paas/v4',values=['low','high','max'],wire='glm')
        for value in ['low','high','max']:
            body = self.request(value)
            self.assertEqual(body['reasoning_effort'],value)
            self.assertEqual(body['extra_body']['thinking']['type'], 'enabled')
            self.assertFalse(body['extra_body']['thinking'].get('clear_thinking', False))

    def test_glm_responses_uses_native_reasoning_object(self):
        self.configure('glm-5.3-flash','https://open.bigmodel.cn/api/v1','codex_responses',values=['low','high','max'],wire='openai_responses')
        self.assertEqual(self.cap['profile'], 'custom')
        for value in ['low','high','max']:
            body = self.request(value)
            self.assertEqual(body['reasoning']['effort'], value)
            self.assertNotIn('reasoning_effort', body)
            self.assertNotIn('thinking', body.get('extra_body', {}))

    def test_qwen_budget(self):
        self.configure('qwen-plus','https://dashscope.aliyuncs.com/compatible-mode/v1',values=['none','low','medium','high'],wire='qwen_budget')
        for value, budget in [('low',1024),('medium',4096),('high',8192)]:
            body = self.request(value)
            self.assertEqual(body['extra_body'], {'enable_thinking':True, 'thinking_budget':budget})
            self.assertNotIn('reasoning_effort',body)
        self.assertEqual(self.request('none')['extra_body'], {'enable_thinking':False})

    def test_gpt_responses_minimal_and_explicit_none(self):
        levels = self.configure('gpt-5','https://api.openai.com/v1','codex_responses',values=['minimal','low','medium','high'],wire='openai_responses')
        # The new Hermes transport clamps minimal to low on the official route.
        if 'minimal' not in levels:
            self.assertEqual(self.request('low')['reasoning']['effort'], 'low')
        else:
            self.assertEqual(self.request('minimal')['reasoning']['effort'], 'minimal')
        self.configure('gpt-5.6-sol','https://api.openai.com/v1','codex_responses')
        self.assertEqual(self.cap['wire'],'openai_responses')
        body = self.request('none',request_overrides={'extra_body':{'reasoning':{'effort':'none'}}})
        self.assertEqual(body['extra_body']['reasoning']['effort'],'none')

    def test_unknown_and_unrelated_custom_route(self):
        self.assertEqual(self.configure('unknown','https://custom.example/v1'),['default'])
        body = self.request('high')
        self.assertNotIn('reasoning_effort',body)
        extra, top = self.profile.build_api_kwargs_extras(model='other',base_url='https://other.example/v1',reasoning_config=parse_reasoning_effort('high'))
        self.assertEqual(top['reasoning_effort'],'high')

    def test_openrouter_metadata_does_not_invent_levels(self):
        self.assertEqual(self.configure('vendor/model','https://openrouter.ai/api/v1',metadata={'supported_parameters':['reasoning']}),['default'])
        levels = self.configure('vendor/model','https://openrouter.ai/api/v1',metadata={'supported_parameters':['reasoning'],'reasoning':{'supported_efforts':['none','low','high'],'mandatory':True}})
        self.assertEqual(levels,['low','high'])
        self.assertEqual(self.request('low')['extra_body']['reasoning']['effort'],'low')

    def test_unrecognized_gpt_does_not_inherit_generic_ladder(self):
        self.assertEqual(self.configure('gpt-5-future','https://api.openai.com/v1'),['default'])

    def test_glm_toggle_has_no_effort(self):
        self.configure('glm-4.7','https://open.bigmodel.cn/api/paas/v4',values=['none','enabled'],wire='glm')
        body = self.request('medium')
        self.assertNotIn('reasoning_effort',body)
        self.assertEqual(body['extra_body']['thinking']['type'], 'enabled')
        self.assertFalse(body['extra_body']['thinking'].get('clear_thinking', False))

    def test_anthropic_metadata_intersects_native_adapter(self):
        self.assertEqual(self.configure('claude-opus-4-6','https://api.anthropic.com/v1','anthropic_messages',values=['low','high','xhigh','max'],wire='anthropic'),['low','high','max'])
        self.assertEqual(self.configure('claude-opus-4-7','https://api.anthropic.com/v1','anthropic_messages',values=['low','high','xhigh','max'],wire='anthropic'),['low','high','xhigh','max'])

    def test_gpt_chat_disable(self):
        self.configure('gpt-5.1','https://api.openai.com/v1',values=['none','low','medium','high'],wire='openai_chat')
        self.assertEqual(self.request('none')['reasoning_effort'],'none')


class NativeBrowserAdapterTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.fixture_root = tempfile.TemporaryDirectory(prefix='easy-stock browser 中文 ')
        root = Path(cls.fixture_root.name)
        cls.real_browser = root / ('browser.exe' if os.name == 'nt' else 'browser')
        source = root / 'main.go'
        source.write_text('''package main
import ("encoding/json"; "fmt"; "os")
func main() {
  log, err := os.OpenFile(os.Getenv("A_STOCK_TEST_CALL_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
  if err != nil { panic(err) }
  json.NewEncoder(log).Encode(os.Args[1:]); log.Close()
  for _, arg := range os.Args[1:] {
    if arg == "load" && os.Getenv("A_STOCK_TEST_STATE_FAIL") == "1" { os.Exit(7) }
  }
  fmt.Print(`{"success":true,"data":{"text":"fixture page"}}`)
}
''', encoding='utf-8')
        built = subprocess.run(['go', 'build', '-o', str(cls.real_browser), str(source)],
                               capture_output=True, env={**os.environ, 'GOMAXPROCS': '2'}, timeout=60)
        if built.returncode:
            cls.fixture_root.cleanup()
            raise RuntimeError(built.stderr.decode('utf-8', 'replace'))
        cls.addClassCleanup(cls.fixture_root.cleanup)

    def setUp(self):
        from tools import browser_tool, browser_tool_install, browser_tool_session
        self.browser, self.discovery, self.session = browser_tool, browser_tool_install, browser_tool_session
        self.enterContext(mock.patch.object(self.discovery, '_find_agent_browser', self.discovery._find_agent_browser))
        self.enterContext(mock.patch.object(self.session, '_agent_browser_argv', self.session._agent_browser_argv))
        self.enterContext(mock.patch.object(self.browser, '_build_browser_env', self.browser._build_browser_env))
        self.enterContext(mock.patch.object(self.session, '_easy_stock_wrapper', None, create=True))
        self.root = Path(self.enterContext(tempfile.TemporaryDirectory(prefix='browser adapter 中文 ')))
        self.wrapper = self.root / 'agent-browser.py'
        original = Path(__file__).resolve().parents[3] / 'desktop/scripts/browser-bin/agent-browser'
        self.wrapper.write_bytes(original.read_bytes())
        self.state = self.root / 'state with spaces.json'
        self.state.write_text('{"cookies":[],"origins":[]}', encoding='utf-8')
        self.log = self.root / 'calls.jsonl'
        self.enterContext(mock.patch.dict(os.environ, {
            'A_STOCK_AGENT_BROWSER_WRAPPER_DIR': str(self.root),
            'A_STOCK_AGENT_BROWSER_REAL': str(self.real_browser),
            'AGENT_BROWSER_STATE': str(self.state),
            'A_STOCK_TEST_CALL_LOG': str(self.log),
            'OPENAI_API_KEY': 'fixture-model-secret',
        }))

    def test_managed_discovery_and_native_argv_load_state_before_navigation(self):
        bridge.install_browser_adapter()
        command = self.discovery._find_agent_browser(validate=False)
        self.assertEqual(command, str(self.wrapper.resolve()))
        self.assertEqual(self.session._agent_browser_argv(command), [sys.executable, command])
        first_argv = self.session._agent_browser_argv
        bridge.install_browser_adapter()
        self.assertIs(self.session._agent_browser_argv, first_argv)
        env = self.browser._build_browser_env()
        self.assertNotIn('OPENAI_API_KEY', env)
        self.assertEqual(env['A_STOCK_AGENT_BROWSER_REAL'], str(self.real_browser))
        self.assertEqual(env['AGENT_BROWSER_STATE'], str(self.state))
        env['AGENT_BROWSER_SOCKET_DIR'] = str(self.root / 'socket')
        env['A_STOCK_TEST_CALL_LOG'] = str(self.log)
        url = 'https://example.invalid/a path?x=100%25&name=中文'
        prefix = self.session._agent_browser_argv(command) + ['--session', 'fixture', '--json']
        for args in (['open', url], ['open', url], ['eval', 'const x = "100% 中文";\nconsole.log(x);']):
            result = subprocess.run(prefix + args, env=env, capture_output=True, timeout=15)
            self.assertEqual(result.returncode, 0, result.stderr.decode('utf-8', 'replace'))
            self.assertTrue(json.loads(result.stdout)['success'])
        calls = [json.loads(line) for line in self.log.read_text(encoding='utf-8').splitlines()]
        self.assertEqual(calls, [
            ['--session', 'fixture', '--json', 'open', 'about:blank'],
            ['--session', 'fixture', '--json', 'state', 'load', str(self.state.resolve())],
            ['--session', 'fixture', '--json', 'open', url],
            ['--session', 'fixture', '--json', 'open', url],
            ['--session', 'fixture', '--json', 'eval', 'const x = "100% 中文";\nconsole.log(x);'],
        ])

    def test_missing_managed_binary_fails_without_external_fallback(self):
        os.environ['A_STOCK_AGENT_BROWSER_REAL'] = str(self.root / 'missing.exe')
        bridge.install_browser_adapter()
        with self.assertRaisesRegex(FileNotFoundError, 'runtime is unavailable'):
            self.discovery._find_agent_browser()

    def test_gateway_installs_browser_adapter_before_entering_gateway(self):
        self.enterContext(mock.patch.dict(os.environ, {
            'EASY_STOCK_REASONING_POLICY': '{"config":{},"capability":{}}',
        }))
        self.enterContext(mock.patch.object(sys, 'argv', ['reasoning_launcher']))
        self.enterContext(mock.patch.object(bridge, 'install_profile'))
        def gateway(module, **options):
            self.assertEqual(module, 'tui_gateway.entry')
            self.assertEqual(options, {'run_name': '__main__'})
            self.assertEqual(self.discovery._find_agent_browser(), str(self.wrapper.resolve()))
        entry = self.enterContext(mock.patch.object(bridge.runpy, 'run_module', side_effect=gateway))
        bridge.main()
        entry.assert_called_once()

    def test_no_managed_wrapper_preserves_external_runtime(self):
        os.environ.pop('A_STOCK_AGENT_BROWSER_WRAPPER_DIR')
        original = self.discovery._find_agent_browser
        bridge.install_browser_adapter()
        self.assertIs(self.discovery._find_agent_browser, original)


class UnattendedOnboardingTests(unittest.TestCase):
    def test_fresh_sandbox_does_not_append_interactive_notes(self):
        from agent import onboarding
        original = onboarding.first_contact_turn_note
        self.addCleanup(setattr, onboarding, 'first_contact_turn_note', original)
        args = ({'onboarding': {'profile_build': 'off'}}, Path('/unused'))
        kwargs = dict(session_history_empty=True, install_has_prior_sessions=False)
        with mock.patch.dict(os.environ, {'EASY_STOCK_UNATTENDED': ''}):
            bridge.install_unattended_policy()
            self.assertIs(onboarding.first_contact_turn_note, original)
            self.assertIn('/help', onboarding.first_contact_turn_note(*args, **kwargs))
        with mock.patch.dict(os.environ, {'EASY_STOCK_UNATTENDED': '1'}):
            bridge.install_unattended_policy()
            self.assertIsNone(onboarding.first_contact_turn_note(*args, **kwargs))
            self.assertIsNone(onboarding.first_contact_turn_note(
                {}, Path('/unused'), **kwargs))


if __name__ == '__main__':
    unittest.main()
