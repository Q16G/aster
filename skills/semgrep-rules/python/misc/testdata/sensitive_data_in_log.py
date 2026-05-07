import logging

logger = logging.getLogger(__name__)


# ruleid: python-misc-sensitive-data-in-log
def log_password(password):
    logger.info("User password: %s", password)


# ruleid: python-misc-sensitive-data-in-log
def log_token(access_token):
    logging.debug("Token: %s", access_token)


# ruleid: python-misc-sensitive-data-in-log
def log_secret_fstring(secret_key):
    logger.error(f"Key = {secret_key}")


# ruleid: python-misc-sensitive-data-in-log
def log_api_key(api_key):
    logger.warn("API key: %s", api_key)


# ok: python-misc-sensitive-data-in-log
def log_normal(username):
    logger.info("User logged in: %s", username)


# ok: python-misc-sensitive-data-in-log
def log_message():
    logger.info("Please enter your password")


# ok: python-misc-sensitive-data-in-log
def log_user_id(user_id):
    logging.debug("Processing user: %s", user_id)
