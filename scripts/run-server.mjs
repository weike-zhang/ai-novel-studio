import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";

const [command, ...args] = process.argv.slice(2);
const valid = (command === "api" || command === "worker") && args.length === 0
  || command === "migrate" && args.length === 1 && ["up", "down", "status"].includes(args[0]);
if (!valid) {
  console.error("Usage: node --env-file=.env scripts/run-server.mjs <api|worker|migrate up|migrate down|migrate status>");
  process.exit(2);
}
const child = spawn("go", ["run", `./cmd/${command}`, ...args], {
  cwd: fileURLToPath(new URL("../services/server/", import.meta.url)),
  stdio: "inherit",
  env: process.env,
});
child.on("error", (error) => { console.error(error.message); process.exitCode = 1; });
child.on("exit", (code, signal) => { process.exitCode = code ?? (signal ? 1 : 0); });
for (const signal of ["SIGINT", "SIGTERM"]) process.on(signal, () => child.kill(signal));
