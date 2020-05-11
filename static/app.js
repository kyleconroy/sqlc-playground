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

  let godoc = document.getElementById('godoc');
  let errors = document.getElementById('errors');
  let stderr = document.getElementById('stderr');

  let loadOutput = function(data) {
    if (data.sha) {
      history.replaceState(data, 'sqlc playground', "/p/"+data.sha)
    }

    if (data.errored) {
      // GoDoc pane
      godoc.classList.add('hidden');
      // Error pane
      errors.classList.remove("hidden");
      stderr.innerText = data.error || "500: Internal Server Error";
    } else {
      // Remove the existing tabs
      let tabs = document.getElementById('codet');
      while (tabs.firstChild) {
        tabs.firstChild.remove();
      }

      // Create documents for each
      for (let i = 0; i < data.files.length; i++) {
        const file = data.files[i];
        const doc = CodeMirror.Doc(file.contents, "text/x-go");

        // Create a new tab for each document
        const a = document.createElement("a");
        a.innerText = file.name;
        a.href = "#output=" + file.name;
        a.onclick = function(e) {
          e.preventDefault();
          
          if (a.classList.contains("selected")) {
            return;
          }
          
          // Set contents of the editor to the selected document
          codeView.swapDoc(doc);

          // Unset the current selected tab
          var selected = document.querySelector("#codet li a.selected");
          if (selected) {
            selected.classList.remove("selected");
          }

          // Set this tab as the selected one
          a.classList.add("selected");
        };

        if (file.name.endsWith(".sql.go")) {
          a.classList.add("selected");
          codeView.swapDoc(doc);
        }

        const li = document.createElement("li");
        li.appendChild(a);
        tabs.appendChild(li);
      }

      godoc.classList.remove('hidden');
      errors.classList.add("hidden");
    }
  };

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
        loadOutput(data);
      })
      .catch((error) => {
        console.error('Error:', error);
      });
    }, 200);
  });

  let response = document.getElementById('response');
  if (response) {
    loadOutput(JSON.parse(response.innerText));
  }
};
