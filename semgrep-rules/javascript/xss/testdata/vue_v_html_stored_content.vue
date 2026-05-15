<template>
  <!-- ruleid: javascript-xss-vue-v-html-stored-content -->
  <article v-html="post.content"></article>

  <!-- ruleid: javascript-xss-vue-v-html-stored-content -->
  <section v-html="renderMarkdown(comment.markdown)"></section>

  <!-- ok: javascript-xss-vue-v-html-stored-content -->
  <div v-html="DOMPurify.sanitize(post.content)"></div>
</template>
