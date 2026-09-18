"""Integration tests against bundled Hermes transports; no network or SDK patches."""
import unittest
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

    def test_deepseek_alias_native_translation(self):
        self.assertEqual(self.configure('deepseek-chat', 'https://api.deepseek.com/v1'), ['none','low','medium','high','max'])
        body = self.request('max')
        self.assertEqual(body['reasoning_effort'], 'max')
        self.assertEqual(body['extra_body']['thinking'], {'type':'enabled'})
        self.assertEqual(self.request('none')['extra_body']['thinking'], {'type':'disabled'})

    def test_kimi_model_specific_levels_and_xor(self):
        for model, levels in [('kimi-k2.6',['none','low','medium','high']), ('kimi-k3',['none','low','high','max'])]:
            with self.subTest(model=model):
                self.assertEqual(self.configure(model,'https://api.moonshot.cn/v1'), levels)
                body = self.request('high')
                self.assertEqual(body['reasoning_effort'], 'high')
                self.assertNotIn('thinking', body.get('extra_body',{}))
                body = self.request('none')
                self.assertNotIn('reasoning_effort',body)
                self.assertEqual(body['extra_body']['thinking'], {'type':'disabled'})

    def test_glm_documented_levels_use_native_wire(self):
        self.configure('glm-5.3-flash','https://open.bigmodel.cn/api/paas/v4',values=['low','high','max'],wire='glm')
        for value in ['low','high','max']:
            body = self.request(value)
            self.assertEqual(body['reasoning_effort'],value)
            self.assertEqual(body['extra_body']['thinking'], {'type':'enabled'})

    def test_qwen_boolean_only(self):
        self.configure('qwen-plus','https://dashscope.aliyuncs.com/compatible-mode/v1',values=['none','enabled'],wire='qwen_toggle')
        for value, enabled in [('none',False),('medium',True)]:
            body = self.request(value)
            self.assertEqual(body['extra_body']['enable_thinking'],enabled)
            self.assertNotIn('reasoning_effort',body)

    def test_gpt_responses_minimal_and_explicit_none(self):
        self.configure('gpt-5','https://api.openai.com/v1','codex_responses',values=['minimal','low','medium','high'],wire='openai_responses')
        self.assertEqual(self.request('minimal')['reasoning']['effort'],'minimal')
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
        self.assertEqual(body['extra_body']['thinking'],{'type':'enabled'})

    def test_anthropic_metadata_intersects_native_adapter(self):
        self.assertEqual(self.configure('claude-opus-4-6','https://api.anthropic.com/v1','anthropic_messages',values=['low','high','xhigh','max'],wire='anthropic'),['low','high','max'])
        self.assertEqual(self.configure('claude-opus-4-7','https://api.anthropic.com/v1','anthropic_messages',values=['low','high','xhigh','max'],wire='anthropic'),['low','high','xhigh','max'])

    def test_gpt_chat_disable(self):
        self.configure('gpt-5.1','https://api.openai.com/v1',values=['none','low','medium','high'],wire='openai_chat')
        self.assertEqual(self.request('none')['reasoning_effort'],'none')


if __name__ == '__main__':
    unittest.main()
