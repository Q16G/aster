import { redirect } from "next/navigation";

function insecureKoa(ctx) {
  // ruleid: javascript-misc-open-redirect-frameworks
  ctx.redirect(ctx.query.next);
}

function insecureFastify(req, reply) {
  const target = req.query.next;
  // ruleid: javascript-misc-open-redirect-frameworks
  reply.redirect(target);
}

function insecureNext(searchParams) {
  // ruleid: javascript-misc-open-redirect-frameworks
  redirect(searchParams.get("next"));
}

function safeKoa(ctx) {
  // ok: javascript-misc-open-redirect-frameworks
  ctx.redirect("/dashboard");
}
