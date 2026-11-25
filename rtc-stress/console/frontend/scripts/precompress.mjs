import { readdir, readFile, writeFile } from "node:fs/promises";
import { gzipSync, constants } from "node:zlib";
const directory = new URL("../dist/assets/", import.meta.url);
let saved = 0;
for (const entry of await readdir(directory, { withFileTypes: true })) {
  if (!entry.isFile() || !/\.(js|css|svg|json)$/.test(entry.name)) continue;
  const source = await readFile(new URL(entry.name, directory));
  const compressed = gzipSync(source, { level: constants.Z_BEST_COMPRESSION });
  await writeFile(new URL(entry.name + ".gz", directory), compressed);
  saved += source.length - compressed.length;
}
console.log(
  `Precompressed static assets; transfer savings ${(saved / 1024).toFixed(0)} KiB.`,
);
