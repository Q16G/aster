import com.googlecode.aviator.AviatorEvaluator;
import com.googlecode.aviator.AviatorEvaluatorInstance;
import com.googlecode.aviator.Expression;
import java.util.HashMap;
import java.util.Map;

class AviatorInjection {

    // ---- Vulnerable: AviatorEvaluator.execute with dynamic input ----

    void insecureExecute(String userInput) {
        // ruleid: java-injection-code-injection-aviator
        AviatorEvaluator.execute(userInput);
    }

    void insecureExecuteWithEnv(String userInput) {
        Map<String, Object> env = new HashMap<>();
        env.put("x", 1);
        // ruleid: java-injection-code-injection-aviator
        AviatorEvaluator.execute(userInput, env);
    }

    // ---- Vulnerable: AviatorEvaluator.compile with dynamic input ----

    void insecureCompile(String userInput) {
        // ruleid: java-injection-code-injection-aviator
        Expression expr = AviatorEvaluator.compile(userInput);
        expr.execute();
    }

    void insecureCompileCached(String userInput) {
        // ruleid: java-injection-code-injection-aviator
        AviatorEvaluator.compile(userInput, true);
    }

    // ---- Vulnerable: fully-qualified class name ----

    void insecureFqn(String userInput) {
        // ruleid: java-injection-code-injection-aviator
        com.googlecode.aviator.AviatorEvaluator.execute(userInput);
    }

    // ---- Vulnerable: AviatorEvaluatorInstance ----

    void insecureInstance(String userInput) {
        AviatorEvaluatorInstance instance = AviatorEvaluator.newInstance();
        // ruleid: java-injection-code-injection-aviator
        instance.execute(userInput);
    }

    void insecureInstanceCompile(String userInput) {
        AviatorEvaluatorInstance instance = AviatorEvaluator.newInstance();
        // ruleid: java-injection-code-injection-aviator
        instance.compile(userInput);
    }

    void insecureNewInstanceDirect(String userInput) {
        // ruleid: java-injection-code-injection-aviator
        AviatorEvaluator.newInstance().execute(userInput);
    }

    // ---- Vulnerable: string concatenation ----

    void insecureConcatExecute(String userInput) {
        String expr = "result = " + userInput + " + 1";
        // ruleid: java-injection-code-injection-aviator
        AviatorEvaluator.execute(expr);
    }

    void insecureConcatCompile(String userInput) {
        String expr = "fn(" + userInput + ")";
        // ruleid: java-injection-code-injection-aviator
        AviatorEvaluator.compile(expr);
    }

    void insecureStringFormat(String userInput) {
        String expr = String.format("a + %s", userInput);
        // ruleid: java-injection-code-injection-aviator
        AviatorEvaluator.execute(expr);
    }

    void insecureInlineConcat(String userInput) {
        // ruleid: java-injection-code-injection-aviator
        AviatorEvaluator.execute("result = " + userInput + " + 1");
    }

    // ---- Safe: hardcoded string literals ----

    void safeHardcodedExecute() {
        // ok: java-injection-code-injection-aviator
        AviatorEvaluator.execute("1 + 2 + 3");
    }

    void safeHardcodedCompile() {
        // ok: java-injection-code-injection-aviator
        AviatorEvaluator.compile("a + b");
    }

    void safeHardcodedWithEnv() {
        Map<String, Object> env = new HashMap<>();
        env.put("a", 1);
        env.put("b", 2);
        // ok: java-injection-code-injection-aviator
        AviatorEvaluator.execute("a + b", env);
    }

    void safeHardcodedFqn() {
        // ok: java-injection-code-injection-aviator
        com.googlecode.aviator.AviatorEvaluator.execute("1 + 2");
    }

    void safeHardcodedInstance() {
        AviatorEvaluatorInstance instance = AviatorEvaluator.newInstance();
        // ok: java-injection-code-injection-aviator
        instance.execute("a + b");
    }

    void safeCompiledExpressionExecute() {
        Expression expr = AviatorEvaluator.compile("a + b");
        Map<String, Object> env = new HashMap<>();
        env.put("a", 1);
        // ok: java-injection-code-injection-aviator
        expr.execute(env);
    }
}
