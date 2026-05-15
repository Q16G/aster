import javax.script.ScriptEngine;
import javax.script.ScriptEngineManager;

class ScriptEngineInjection {
    void insecure(String expression) throws Exception {
        ScriptEngine engine = new ScriptEngineManager().getEngineByName("groovy");
        // ruleid: java-injection-code-injection-script-engine
        engine.eval(expression);
    }

    void insecureManager(String expression) throws Exception {
        ScriptEngineManager manager = new ScriptEngineManager();
        // ruleid: java-injection-code-injection-script-engine
        manager.getEngineByName("javascript").eval(expression);
    }

    void safe() throws Exception {
        ScriptEngine engine = new ScriptEngineManager().getEngineByName("groovy");
        // ok: java-injection-code-injection-script-engine
        engine.eval("1 + 1");
    }
}
