
// Checks the prover endpoints before inserting into the database, if not valid throw an error
onRecordBeforeCreateRequest((e) => {
  let validProverEndpoint = false;

  try {
    // Remove trailing slashes
    const newProverEndpoint = e.record.get('url');

    const response = $http.send({
      url:     `${newProverEndpoint}/status`,
      method:  "GET",
      body:    "",
      headers: {"content-type": "application/json"},
      timeout: 120, // in seconds
    });

    if (response.statusCode == 200) {
      const data = response.json;

      // Check if the endpoint is valid and contains the minProofFee and currentCapacity
      if('minProofFee' in data && 'currentCapacity' in data){
        console.log('creating the prover');
        validProverEndpoint = true;
      }
    }
  } catch (error) {
    validProverEndpoint = false;
  }

  if(!validProverEndpoint){
    throw new BadRequestError("Failed to create prover endpoint, not a valid endpoint");
  }
}, "prover_endpoints")
