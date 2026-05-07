package com.example.testdata;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

public class SensitiveDataInLog {

    private static final Logger logger = LoggerFactory.getLogger(SensitiveDataInLog.class);

    // ruleid: java-misc-sensitive-data-in-log
    public void logPassword(String password) {
        logger.info("User login with password: {}", password);
    }

    // ruleid: java-misc-sensitive-data-in-log
    public void logToken(String accessToken) {
        logger.debug("Token issued: {}", accessToken);
    }

    // ruleid: java-misc-sensitive-data-in-log
    public void logSecretConcat(String secretKey) {
        logger.error("Key = " + secretKey);
    }

    // ruleid: java-misc-sensitive-data-in-log
    public void logApiKey(String apiKey) {
        logger.warn("API key received: {}", apiKey);
    }

    // ok: java-misc-sensitive-data-in-log
    public void logNormal(String username) {
        logger.info("User logged in: {}", username);
    }

    // ok: java-misc-sensitive-data-in-log
    public void logMessage() {
        logger.info("Please enter your password to continue");
    }

    // ok: java-misc-sensitive-data-in-log
    public void logUserId(int userId) {
        logger.debug("Processing request for user: {}", userId);
    }
}
