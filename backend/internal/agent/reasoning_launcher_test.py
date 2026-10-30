"""Integration tests against bundled Hermes transports; no network or SDK patches."""
import json
import os
import subprocess
import sys
import unittest
from pathlib import Path
from unittest.mock import patch
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


class UnattendedOnboardingTests(unittest.TestCase):
    def test_fresh_sandbox_does_not_append_interactive_notes(self):
        from agent import onboarding
        original = onboarding.first_contact_turn_note
        self.addCleanup(setattr, onboarding, 'first_contact_turn_note', original)
        args = ({'onboarding': {'profile_build': 'off'}}, Path('/unused'))
        kwargs = dict(session_history_empty=True, install_has_prior_sessions=False)
        with patch.dict(os.environ, {'EASY_STOCK_UNATTENDED': ''}):
            bridge.install_unattended_policy()
            self.assertIs(onboarding.first_contact_turn_note, original)
            self.assertIn('/help', onboarding.first_contact_turn_note(*args, **kwargs))
        with patch.dict(os.environ, {'EASY_STOCK_UNATTENDED': '1'}):
            bridge.install_unattended_policy()
            self.assertIsNone(onboarding.first_contact_turn_note(*args, **kwargs))
            self.assertIsNone(onboarding.first_contact_turn_note(
                {}, Path('/unused'), **kwargs))


if __name__ == '__main__':
    unittest.main()
