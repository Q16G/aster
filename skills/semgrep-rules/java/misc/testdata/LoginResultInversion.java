class LoginResultInversion {
    boolean login(User user) {
        // ruleid: java-misc-login-result-inversion
        if (user != null) {
            return false;
        } else {
            return true;
        }
    }

    boolean authenticate(User user) {
        // ruleid: java-misc-login-result-inversion
        if (user != null && !user.getAccount().equals("")) {
            return false;
        } else {
            return true;
        }
    }

    Result doLogin(User user) {
        // ruleid: java-misc-login-result-inversion
        if (user != null) {
            return Result.error("login failed");
        }
        return Result.ok();
    }

    boolean processLogin(java.util.List<User> users) {
        // ruleid: java-misc-login-result-inversion
        if (users.isEmpty()) {
            return true;
        }
        return false;
    }

    boolean safeLogin(User user) {
        // ok: java-misc-login-result-inversion
        if (user != null) {
            return true;
        }
        return false;
    }

    Result safeDoLogin(User user) {
        // ok: java-misc-login-result-inversion
        if (user != null) {
            return Result.ok();
        }
        return Result.error("user not found");
    }
}

class User {
    String getAccount() { return ""; }
}

class Result {
    static Result ok() { return new Result(); }
    static Result error(String msg) { return new Result(); }
}
