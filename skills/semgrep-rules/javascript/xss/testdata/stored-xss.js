import DOMPurify from "dompurify";
import markdownit from "markdown-it";

function renderStoredPost(post, el) {
  // ruleid: javascript-xss-stored-content-render
  el.innerHTML = post.content;
}

function renderStoredComment(comment) {
  // ruleid: javascript-xss-stored-content-render
  return <div dangerouslySetInnerHTML={{ __html: comment.body }} />;
}

function renderSanitized(post, el) {
  // ok: javascript-xss-stored-content-render
  el.innerHTML = DOMPurify.sanitize(post.content);
}

function previewMarkdown(article, el) {
  // ruleid: javascript-xss-markdown-raw-render
  el.innerHTML = marked.parse(article.markdown);
}

function previewMarkdownWithRenderer(article) {
  const md = markdownit({ html: true, linkify: true });
  // ruleid: javascript-xss-markdown-raw-render
  return <section dangerouslySetInnerHTML={{ __html: md.render(article.markdown) }} />;
}

function safeMarkdown(article, el) {
  // ok: javascript-xss-markdown-raw-render
  el.innerHTML = DOMPurify.sanitize(marked.parse(article.markdown));
}
