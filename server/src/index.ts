import { readRelayServerConfig, startRelayServer } from "./relay-server.js";

const config = readRelayServerConfig();
const server = await startRelayServer(config);

console.log(
  JSON.stringify({
    apiPort: server.apiPort,
    wsPort: server.wsPort,
  }),
);
