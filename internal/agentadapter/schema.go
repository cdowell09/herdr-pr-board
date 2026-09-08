package agentadapter

// ResultSchema constrains agent output. The reviewer contract still validates
// the exact captured revision and terminal outcome before writing the local result atomically.
const ResultSchema = `{
 "type":"object","additionalProperties":false,
 "required":["version","identity","base_oid","outcome"],
 "properties":{
  "version":{"type":"integer","enum":[1]},
  "identity":{
   "type":"object","additionalProperties":false,
   "required":["repository","number","head_oid","base_ref_name"],
   "properties":{
    "repository":{"type":"string"},"number":{"type":"integer","minimum":1},
    "head_oid":{"type":"string"},"base_ref_name":{"type":"string"}
   }
  },
  "base_oid":{"type":"string"},
  "outcome":{
   "type":"object","additionalProperties":false,
   "required":["status","message","findings"],
   "properties":{
    "status":{"type":"string","enum":["completed","blocked","failed"]},
    "message":{"type":"string"},
    "findings":{"type":"array","items":{
     "type":"object","additionalProperties":false,
     "required":["severity","title","body","path","line"],
     "properties":{
      "severity":{"type":"string","enum":["P0","P1","P2","P3"]},
      "title":{"type":"string"},"body":{"type":"string"},
      "path":{"type":["string","null"]},
      "line":{"type":["integer","null"],"minimum":1}
     }
    }}
   }
  }
 }
}`
