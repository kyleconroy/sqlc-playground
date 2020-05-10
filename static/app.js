window.onload = function() {
  let sqlEditor = CodeMirror.fromTextArea(document.getElementById('sql'), {
    mode: 'text/x-pgsql',
    indentWithTabs: true,
    smartIndent: true,
    lineNumbers: true,
    matchBrackets : true,
    autofocus: true, 
    viewportMargin: Infinity,
  });

  let codeView = CodeMirror.fromTextArea(document.getElementById('go'), {
    mode: 'text/x-go',
    indentWithTabs: true,
    smartIndent: true,
    lineNumbers: true,
    matchBrackets : true,
    autofocus: false, 
    readOnly: true,
    viewportMargin: Infinity,
  });

  let lastTimeout = 0;
  sqlEditor.on("change", function(instance, change) {
    if (lastTimeout > 0) {
        clearTimeout(lastTimeout);
    }
    lastTimeout = setTimeout(function() {
      fetch('/generate', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          query: instance.getDoc().getValue(),
        }),
      })
      .then((response) => {
        return response.json();
      })
      .then((data) => {
        let godoc = document.getElementById('godoc');
        let errors = document.getElementById('errors');
        let stderr = document.getElementById('stderr');

        // if (data.sha) {
        //   history.replaceState({}, '', "/p/"+data.sha)
        // }

        if (data.errored) {
          // GoDoc pane
          godoc.classList.add('hidden');
          // Error pane
          errors.classList.remove("hidden");
          stderr.innerText = data.error || "500: Internal Server Error";
        } else {
          // GoDoc pane
          godoc.classList.remove('hidden');
          codeView.getDoc().setValue(data.files[0].contents);
          // Error pane
          errors.classList.add("hidden");
        }
      })
      .catch((error) => {
        console.error('Error:', error);
      });
    }, 200);
  });
};
