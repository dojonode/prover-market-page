
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
      timeout: 30, // in seconds
    });

    if (response.statusCode == 200) {
      const data = response.json;

      // Check if the endpoint is valid and contains the minProofFee and currentCapacity
      if('minSgxTierFee' in data){
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

// Custom endpoint that goes through the endpoints and checks if they are online + get the latest minimum fee/capacity
routerAdd("get", "/validProvers", (c) => {
  const records = arrayOf(new Record);

  $app.dao().recordQuery("prover_endpoints")
      .limit(100)
      .all(records);

  let recordsResult = [];

  records.forEach(record => {
    try{
      const response = $http.send({
        url:     `${record.get('url')}/status`,
        method:  "GET",
        body:    "",
        headers: {"content-type": "application/json"},
        timeout: 4, // in seconds
      });

      if (response.statusCode == 200) {
        const data = response.json;

        // Check if the endpoint is valid and contains the minProofFee and currentCapacity
        if('minSgxTierFee' in data){
          const validProver = {
            url: record.get('url'),
            minimumGas: data.minSgxTierFee,
          };

          recordsResult = [...recordsResult, validProver];
        }
      }
    } catch(error){
      // prover is not valid, we just skip it
    }
  });

  return c.json(200, recordsResult)
})
