Deno.serve((req: Request) => {
  const url = new URL(req.url);
  return new Response(
    JSON.stringify({
      ok: true,
      method: req.method,
      path: url.pathname,
      source: "supadupa-compat-runner",
    }),
    {
      status: 200,
      headers: { "Content-Type": "application/json" },
    },
  );
});
