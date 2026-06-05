export function parseKeyValueLines(value: string) {
  return value
    .split(/\n|,/)
    .map((line) => line.trim())
    .filter(Boolean)
    .reduce<Record<string, string>>((acc, line) => {
      const [key, ...rest] = line.split("=");
      if (key && rest.length > 0) {
        acc[key.trim()] = rest.join("=").trim();
      }
      return acc;
    }, {});
}

export function parseLines(value: string) {
  return value
    .split(/\n|,/)
    .map((line) => line.trim())
    .filter(Boolean);
}
