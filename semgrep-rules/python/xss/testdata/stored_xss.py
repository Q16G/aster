from django.utils.safestring import mark_safe
from markupsafe import Markup
import bleach


def render_profile(profile):
    # ruleid: python-xss-django-stored-content-safe
    return mark_safe(profile.bio)


def render_comment(comment):
    # ruleid: python-xss-django-stored-content-safe
    return mark_safe(comment.rendered_html)


def render_clean_comment(comment):
    # ok: python-xss-django-stored-content-safe
    return mark_safe(bleach.clean(comment.rendered_html))


def render_flask_post(post):
    # ruleid: python-xss-flask-stored-content-markup
    return Markup(post.body)


def render_flask_page(page):
    # ruleid: python-xss-flask-stored-content-markup
    return Markup(page.html)


def render_flask_clean(page):
    # ok: python-xss-flask-stored-content-markup
    return Markup(bleach.clean(page.html))
