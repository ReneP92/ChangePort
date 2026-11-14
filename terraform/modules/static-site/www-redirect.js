// A CloudFront Function on viewer requests (terraform/modules/static-site):
// sends www.${domain} to ${domain} with a permanent redirect, keeping the
// path and the query, and lets every other request through.
function handler(event) {
  var request = event.request;
  if (request.headers.host.value !== "www.${domain}") {
    return request;
  }
  var query = Object.keys(request.querystring)
    .map(function (key) {
      var parameter = request.querystring[key];
      var values = parameter.multiValue ? parameter.multiValue : [parameter];
      return values
        .map(function (value) {
          return value.value === "" ? key : key + "=" + value.value;
        })
        .join("&");
    })
    .join("&");
  return {
    statusCode: 301,
    statusDescription: "Moved Permanently",
    headers: {
      location: { value: "https://${domain}" + request.uri + (query ? "?" + query : "") },
    },
  };
}
