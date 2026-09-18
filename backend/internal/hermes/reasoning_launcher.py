"""Bridge easy-stock to Hermes' provider registry and reasoning vocabulary.

Uses Hermes extension hooks, never SDK monkey-patching. `describe` reads only
supplied catalogs/local Hermes code; gateway mode installs the same adapter.
"""
import copy
import json
import os
import runpy
import sys
import types


def describe(config, metadata=None, supplement=None):
    from agent import reasoning_effort as effort
    from agent.model_metadata import _infer_provider_from_url
    from providers import get_provider_profile
    from hermes_constants import parse_reasoning_effort

    model, base = config["model"], config["base_url"]
    mode = config.get("api_mode") or "chat_completions"
    provider = _infer_provider_from_url(base) or ""
    profile = get_provider_profile(provider)
    from hermes_cli.model_normalize import normalize_model_for_provider
    effective_model = normalize_model_for_provider(model, provider) if provider else model
    result = {"options": [{"value": "default", "label": "暂不支持调节"}], "default": "default",
              "source": "unknown", "note": "Hermes 尚未声明该模型在当前接口上的可调档位，使用运行时默认设置。", "profile": "", "effective_model": effective_model}
    values = []
    if provider in ("openrouter", "nous") and metadata:
        from hermes_cli.models_reasoning_caps import parse_openrouter_reasoning_capabilities
        caps = parse_openrouter_reasoning_capabilities(metadata)
        if caps and caps.get("supported_efforts"):
            values = [v for v in effort.OPENAI_COMPAT_WIRE_EFFORTS if v in caps["supported_efforts"]]
            if caps.get("mandatory"):
                values = [v for v in values if v != "none"]
            result["source"] = "model_api"
    elif mode == "codex_responses" and provider == "openai" and (model in ("gpt-5.6", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna") or effort.is_astra_model(model)):
        values = list(effort.codex_supported_efforts(model))
        result["profile"] = "custom"
        result["wire"] = "openai_responses" if mode == "codex_responses" else "openai_chat"
    elif mode == "chat_completions" and provider in ("kimi-coding", "kimi-coding-cn") and model.lower().startswith(("kimi-k", "k3")):
        # The per-model helper is more specific than KimiProfile's K3 default.
        values = ["none", *effort.kimi_supported_efforts(model)]
    elif mode == "chat_completions" and profile and provider in ("zai", "deepseek"):
        # Inspect the provider's actual translation, excluding compatibility
        # aliases that collapse to another displayed level. No second level table.
        for value in ("none", "minimal", "low", "medium", "high", "xhigh", "max"):
            extra, top = profile.build_api_kwargs_extras(
                reasoning_config=parse_reasoning_effort(value), model=effective_model, base_url=base)
            if top.get("reasoning_effort") == value:
                values.append(value)
            elif value == "none" and extra.get("thinking", {}).get("type") == "disabled":
                values.append(value)
        if values == ["none"]:
            values.append("enabled")
    if values:
        result.update(profile=result["profile"] or provider, source=result["source"] if result["source"] == "model_api" else "hermes",
                      note="复用内置 Hermes 的服务商适配；仅显示不同的有效档位。")
    # Explicit endpoint corrections / capabilities absent from Hermes. Preserve
    # native profile translation whenever it already emits the right schema.
    if supplement and supplement.get("source") != "unknown":
        values = [o["value"] for o in supplement["options"]]
        if mode == "anthropic_messages":
            from agent.anthropic_adapter import _thinking_kwargs
            values = [v for v in values if
                      _thinking_kwargs(parse_reasoning_effort(v), model, 4096).get("output_config", {}).get("effort") == v]
            if not values:
                return result
        result.update(supplement)
        result["profile"] = provider if profile else ("custom" if provider == "openai" else "")
    if not values:
        return result
    labels = {"none": "关闭思考", "enabled": "开启思考", "minimal": "极简", "low": "低", "medium": "中", "high": "高", "xhigh": "极高", "max": "最大"}
    result["options"] = [{"value": v, "label": labels[v]} for v in values]
    if result["default"] not in values:
        result["default"] = "medium" if "medium" in values else "high" if "high" in values else values[-1]
    return result


def install_profile(config, capability):
    from providers import get_provider_profile, register_provider
    from hermes_constants import parse_reasoning_effort

    original = get_provider_profile("custom")
    managed = copy.copy(original)
    native = get_provider_profile(capability.get("profile", ""))
    # The supplied catalog was already checked by describe. Keep the native
    # wire adapter, but avoid re-fetching a different catalog during a request.
    if native and native.name == "openrouter" and capability.get("source") == "model_api":
        native = copy.copy(native)
        native._clamp_reasoning_to_catalog = lambda cfg, model: cfg
    model, base = config["model"], config["base_url"].rstrip("/")
    effective_model = capability.get("effective_model") or model
    values = [o["value"] for o in capability.get("options", [])]
    wire = capability.get("wire", "")

    def matches(context):
        return context.get("model") in (model, effective_model) and str(context.get("base_url", "")).rstrip("/") == base

    def extras(self, *, reasoning_config=None, **context):
        if not matches(context):
            return original.build_api_kwargs_extras(reasoning_config=reasoning_config, **context)
        if values == ["default"]:
            return {}, {}
        selected = (reasoning_config or {}).get("effort", "")
        if (reasoning_config or {}).get("enabled") is False:
            selected = "none"
        if "enabled" in values and selected != "none":
            selected = "enabled"
        if selected not in values:
            selected = capability["default"]
        if wire == "qwen_toggle":
            return {"enable_thinking": selected != "none"}, {}
        normalized = parse_reasoning_effort("medium" if selected == "enabled" else selected)
        if native and native.name in ("kimi-coding", "kimi-coding-cn"):
            from agent import reasoning_effort as effort
            supported = effort.kimi_supported_efforts(context.get("model"))
            overrides = effort.KIMI_K3_OVERRIDES if supported == effort.KIMI_K3_EFFORTS else None
            return effort.thinking_toggle_extras(normalized, supported, overrides)
        if native:
            return native.build_api_kwargs_extras(reasoning_config=normalized, **{**context, "model": effective_model, "supports_reasoning": True})
        return {}, {}

    def extra_body(self, **context):
        if not matches(context):
            return original.build_extra_body(**context)
        if values == ["default"] or not native:
            return {}
        return native.build_extra_body(**context)

    def supported(self, requested_model):
        if requested_model not in (model, effective_model):
            return original.supported_reasoning_efforts(requested_model)
        return tuple(v for v in values if v not in ("default", "enabled"))

    # Named connections resolve to provider="custom" in Hermes. Register only
    # in this child process, delegating all other routes to the original profile.
    managed.build_api_kwargs_extras = types.MethodType(extras, managed)
    managed.build_extra_body = types.MethodType(extra_body, managed)
    managed.supported_reasoning_efforts = types.MethodType(supported, managed)
    register_provider(managed)
    return managed


def main():
    if len(sys.argv) > 1 and sys.argv[1] == "describe":
        request = json.load(sys.stdin)
        result = {}
        for item in request["models"]:
            config = {**request["config"], "model": item["model"]}
            result[item["model"]] = describe(config, item.get("metadata"), item.get("supplement"))
        print(json.dumps(result))
        return
    policy = json.loads(os.environ["EASY_STOCK_REASONING_POLICY"])
    install_profile(policy["config"], policy["capability"])
    runpy.run_module("tui_gateway.entry", run_name="__main__")


if __name__ == "__main__":
    main()
