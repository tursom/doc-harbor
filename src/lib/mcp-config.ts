/** 示例只引用用户自己设置的环境变量，签发的令牌不会被插入页面命令。 */
export function mcpClientConfigs(address: string) {
  const shellURL = `'${address.replaceAll("'", "'\\''")}'`
  return {
    pi: `pi mcp add docharbor --url ${shellURL} --exposure direct --bearer-token-env-var DOCHARBOR_TOKEN`,
    codex: `[mcp_servers.docharbor]\nurl = ${JSON.stringify(address)}\nbearer_token_env_var = "DOCHARBOR_TOKEN"`,
    claude: `claude mcp add --transport http docharbor ${shellURL} --header "Authorization: Bearer \${DOCHARBOR_TOKEN}"`
  }
}
