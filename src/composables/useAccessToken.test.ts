import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../api'
import { useAccessToken } from './useAccessToken'
import { mcpClientConfigs } from '../lib/mcp-config'

afterEach(() => vi.restoreAllMocks())
describe('MCP tokens', () => {
  it('allows 90 days only for docs.read and does not silently shorten mixed-capability tokens', async () => {
    const create = vi.spyOn(api, 'createAccessToken').mockResolvedValue({ token: 'fictional-token', expires_at: '', capabilities: ['docs.read'], scope: {} })
    const token = useAccessToken()
    token.accessTokenForm.value.capabilities = ['docs.read']
    token.accessTokenForm.value.ttl_seconds = 7776000
    await token.createAccessToken()
    expect(create).toHaveBeenCalledWith({ ttl_seconds: 7776000, capabilities: ['docs.read'], scope: { viewer_key: undefined } })
    token.accessTokenForm.value.capabilities.push('ai.diagnostics.read')
    await token.createAccessToken()
    expect(create).toHaveBeenCalledTimes(1)
    expect(token.accessTokenForm.value.ttl_seconds).toBe(7776000)
    expect(token.ttlValidation.value).toContain('最长 24 小时')
    token.accessTokenForm.value.ttl_seconds = 86400
    await token.createAccessToken()
    expect(create).toHaveBeenCalledTimes(2)
  })
  it('rejects empty capabilities and keeps client examples independent of an issued token', async () => {
    const create = vi.spyOn(api, 'createAccessToken')
    const token = useAccessToken()
    token.accessTokenForm.value.capabilities = []
    await token.createAccessToken()
    expect(create).not.toHaveBeenCalled()
    const configs = mcpClientConfigs('https://docs.example.test/api/mcp')
    expect(configs.pi).toContain('--exposure direct --bearer-token-env-var DOCHARBOR_TOKEN')
    expect(configs.codex).toContain('bearer_token_env_var = "DOCHARBOR_TOKEN"')
    expect(configs.claude).toContain('${DOCHARBOR_TOKEN}')
  })
})
