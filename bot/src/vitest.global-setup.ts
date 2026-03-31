import { ensureIntegrationTestPrerequisites } from "./integration-test-setup.js";

export default async function globalSetup(): Promise<void> {
  await ensureIntegrationTestPrerequisites();
}
